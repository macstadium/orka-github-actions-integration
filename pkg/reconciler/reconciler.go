package reconciler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/metrics"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	provisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type vmState int

const (
	vmStateActive        vmState = iota // run.sh running → adopt
	vmStateRunComplete                  // run.sh exited cleanly → cleanup
	vmStateNeedsDeletion                // setup incomplete or run.sh crashed → delete
)

type VMReconciler struct {
	actionsClient actions.ActionsService
	orkaClient    orka.OrkaService
	provisioner   *provisioner.RunnerProvisioner
	adopt         func(vmName string)
	envData       *env.Data
	metrics       orphanRecorder
	logger        *zap.SugaredLogger
}

// orphanRecorder is the slice of *metrics.Metrics the reconciler records into. A nil
// *metrics.Metrics satisfies it and records nothing.
type orphanRecorder interface {
	AddEmulatorOrphansReaped(count int)
}

var _ orphanRecorder = (*metrics.Metrics)(nil)

// NewVMReconciler builds a reconciler. m may be nil, in which case nothing is recorded.
func NewVMReconciler(actionsClient actions.ActionsService, orkaClient orka.OrkaService, p *provisioner.RunnerProvisioner, adopt func(string), envData *env.Data, m *metrics.Metrics) *VMReconciler {
	return &VMReconciler{
		actionsClient: actionsClient,
		orkaClient:    orkaClient,
		provisioner:   p,
		adopt:         adopt,
		envData:       envData,
		metrics:       m,
		logger:        logging.Logger.Named("vm-reconciler"),
	}
}

// VMs must be captured before message processing starts to avoid reconciling
// VMs provisioned by the current process.
func (r *VMReconciler) ReconcileVMs(ctx context.Context, vms []*orka.OrkaVMInfo, scaleSetName string) {
	if len(vms) > 0 {
		r.logger.Infof("reconciliation: found %d existing VM(s) to reconcile", len(vms))

		for _, vm := range vms {
			r.reconcileVM(ctx, vm)
		}

		r.logger.Infof("reconciliation: completed")
	}

	// Runs after adoption, because message processing has already started: a job completion for an
	// active VM that arrives before the VM is adopted finds no runner context to cancel, and the VM
	// stays up until the VM tracker deletes it.
	//
	// Runs even with no VMs to reconcile: an emulator can outlive every VM in the scale set, which
	// is exactly the state this sweep exists to clear. It also runs whether or not any emulator
	// config pairs with this runner today, since one may have been removed while its emulators
	// were still up.
	r.sweepOrphanedEmulators(ctx, scaleSetName)
}

// sweepOrphanedEmulators deletes emulators whose paired VM belonged to this scale set but no longer
// exists. Deleting a VM garbage-collects its emulators through the owner reference, so this covers
// only the gap that leaves behind: on a deploy timeout the CLI abandons the emulator record rather
// than removing it, and the process may have exited before cleanup ran.
//
// VM reconciliation deletes VMs in the background, so one may disappear while this runs. Its
// emulators are then deleted here rather than by the owner-reference cascade, which is harmless.
//
// On a cluster that predates emulators the list fails, so this logs one warning per startup and
// moves on; it never blocks VM reconciliation.
func (r *VMReconciler) sweepOrphanedEmulators(ctx context.Context, scaleSetName string) {
	emulators, err := r.orkaClient.ListEmulators(ctx)
	if err != nil {
		r.logger.Warnf("reconciliation: unable to list Android emulators, skipping the emulator orphan sweep (expected on clusters without emulator support): %v", err)
		return
	}

	if len(emulators) == 0 {
		return
	}

	vms, err := r.orkaClient.ListVMs(ctx, scaleSetName)
	if err != nil {
		r.logger.Warnf("reconciliation: unable to list VMs, skipping emulator orphan sweep: %v", err)
		return
	}

	live := make(map[string]bool, len(vms))
	for _, vm := range vms {
		live[vm.Name] = true
	}

	orphaned := orphanedEmulators(emulators, live, scaleSetName)
	if len(orphaned) == 0 {
		return
	}

	r.logger.Infof("reconciliation: deleting %d orphaned emulator(s) whose paired VM is gone: %s", len(orphaned), strings.Join(orphaned, ", "))

	reaped := 0
	for _, name := range orphaned {
		if err := r.orkaClient.DeleteEmulator(ctx, name); err != nil {
			r.logger.Warnf("reconciliation: failed to delete orphaned emulator %s: %v", name, err)
			continue
		}
		reaped++
	}

	r.metrics.AddEmulatorOrphansReaped(reaped)
}

// orphanedEmulators selects emulators whose paired VM was generated for this scale set and is no
// longer live. Ownership is decided on exact name boundaries, so scale set foo never touches the
// emulators of foo-bar's VMs, nor those of a non-CI workload sharing the namespace.
func orphanedEmulators(emulators []*orka.OrkaEmulatorResponseModel, live map[string]bool, scaleSetName string) []string {
	var orphaned []string
	for _, emulator := range emulators {
		if emulator == nil {
			continue
		}

		if orka.BelongsToScaleSet(emulator.VM, scaleSetName) && !live[emulator.VM] {
			orphaned = append(orphaned, emulator.Name)
		}
	}

	return orphaned
}

func (r *VMReconciler) reconcileVM(ctx context.Context, vm *orka.OrkaVMInfo) {
	runner, err := r.actionsClient.GetRunner(ctx, vm.Name)
	if err != nil {
		r.logger.Warnf("reconciliation: VM %s GitHub check failed, adopting as potential orphan: %v", vm.Name, err)
		r.adopt(vm.Name)
		return
	}

	if runner == nil {
		r.logger.Infof("reconciliation: VM %s has no GitHub runner, deleting", vm.Name)
		go r.deleteVM(ctx, vm.Name)
		return
	}

	state, err := r.checkVMState(vm)
	if err != nil {
		r.logger.Warnf("reconciliation: VM %s state check failed, adopting as potential orphan: %v", vm.Name, err)
		r.adopt(vm.Name)
		return
	}

	switch state {
	case vmStateRunComplete:
		r.logger.Infof("reconciliation: VM %s run completed without cleanup, cleaning up", vm.Name)
		go r.provisioner.CleanupResources(context.WithoutCancel(ctx), vm.Name)
	case vmStateNeedsDeletion:
		r.logger.Infof("reconciliation: VM %s setup incomplete or run.sh crashed, deleting", vm.Name)
		go r.deleteVM(ctx, vm.Name)
	case vmStateActive:
		r.logger.Infof("reconciliation: VM %s is active, adopting for cleanup", vm.Name)
		r.adopt(vm.Name)
	}
}

func (r *VMReconciler) checkVMState(vm *orka.OrkaVMInfo) (vmState, error) {
	if vm.SSH == nil {
		return vmStateNeedsDeletion, fmt.Errorf("VM %s has no SSH port", vm.Name)
	}

	vmIP, err := r.resolveVMIP(vm.IP)
	if err != nil {
		return vmStateNeedsDeletion, err
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", vmIP, *vm.SSH), &ssh.ClientConfig{
		User: r.envData.OrkaVMUsername,
		Auth: []ssh.AuthMethod{ssh.Password(r.envData.OrkaVMPassword)},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return vmStateNeedsDeletion, fmt.Errorf("SSH connect failed: %v", err)
	}
	defer client.Close()

	setupComplete, err := r.fileExists(client, provisioner.SentinelSetupComplete)
	if err != nil {
		return vmStateNeedsDeletion, fmt.Errorf("setup sentinel check failed: %w", err)
	}
	if !setupComplete {
		r.logger.Infof("reconciliation: VM %s setup is not complete, deleting", vm.Name)
		return vmStateNeedsDeletion, nil
	}

	runComplete, err := r.fileExists(client, provisioner.SentinelRunComplete)
	if err != nil {
		return vmStateNeedsDeletion, fmt.Errorf("run sentinel check failed: %w", err)
	}
	if runComplete {
		r.logger.Infof("reconciliation: VM %s run.sh completed, cleaning up", vm.Name)
		return vmStateRunComplete, nil
	}

	processRunning, err := r.isProcessRunning(client, "actions-runner/run.sh")
	if err != nil {
		return vmStateNeedsDeletion, fmt.Errorf("run.sh process check failed: %w", err)
	}
	if !processRunning {
		r.logger.Infof("reconciliation: VM %s run.sh is not running, deleting", vm.Name)
		return vmStateNeedsDeletion, nil
	}

	return vmStateActive, nil
}

func (r *VMReconciler) fileExists(client *ssh.Client, path string) (bool, error) {
	return r.runBoolCheck(client, fmt.Sprintf("test -f %s", path))
}

func (r *VMReconciler) isProcessRunning(client *ssh.Client, pattern string) (bool, error) {
	return r.runBoolCheck(client, fmt.Sprintf("pgrep -f %q", pattern))
}

// runBoolCheck runs a remote command whose exit status encodes a boolean:
// 0 means true, 1 means a clean negative (file missing / no process matched),
// anything else (SSH failure, command error) is returned as an error so a
// failed check is never mistaken for a negative result.
func (r *VMReconciler) runBoolCheck(client *ssh.Client, cmd string) (bool, error) {
	session, err := client.NewSession()
	if err != nil {
		return false, fmt.Errorf("ssh session failed: %w", err)
	}
	defer session.Close()

	err = session.Run(cmd)
	if err == nil {
		return true, nil
	}

	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitStatus() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("%q failed: %w", cmd, err)
}

func (r *VMReconciler) resolveVMIP(vmIP string) (string, error) {
	if !r.envData.OrkaEnableNodeIPMapping {
		return vmIP, nil
	}
	if r.envData.OrkaNodeIPMapping[vmIP] == "" {
		return "", fmt.Errorf("unable to retrieve VM IP from node IP mapping")
	}
	return r.envData.OrkaNodeIPMapping[vmIP], nil
}

func (r *VMReconciler) deleteVM(ctx context.Context, vmName string) {
	r.provisioner.CleanupResources(context.WithoutCancel(ctx), vmName)
}
