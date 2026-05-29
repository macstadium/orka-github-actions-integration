package reconciler

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
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
	provisioner   *provisioner.RunnerProvisioner
	adopt         func(vmName string)
	envData       *env.Data
	logger        *zap.SugaredLogger
}

func NewVMReconciler(actionsClient actions.ActionsService, p *provisioner.RunnerProvisioner, adopt func(string), envData *env.Data) *VMReconciler {
	return &VMReconciler{
		actionsClient: actionsClient,
		provisioner:   p,
		adopt:         adopt,
		envData:       envData,
		logger:        logging.Logger.Named("vm-reconciler"),
	}
}

// VMs must be captured before message processing starts to avoid reconciling
// VMs provisioned by the current process.
func (r *VMReconciler) ReconcileVMs(ctx context.Context, vms []*orka.OrkaVMInfo) {
	if len(vms) == 0 {
		return
	}
	r.logger.Infof("reconciliation: found %d existing VM(s) to reconcile", len(vms))

	for _, vm := range vms {
		r.reconcileVM(ctx, vm)
	}

	r.logger.Infof("reconciliation: completed")
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

	if !r.fileExists(client, provisioner.SentinelSetupComplete) {
		return vmStateNeedsDeletion, nil
	}

	if r.fileExists(client, provisioner.SentinelRunComplete) {
		return vmStateRunComplete, nil
	}

	if !r.isProcessRunning(client, "actions-runner/run.sh") {
		return vmStateNeedsDeletion, nil
	}

	return vmStateActive, nil
}

func (r *VMReconciler) fileExists(client *ssh.Client, path string) bool {
	session, err := client.NewSession()
	if err != nil {
		return false
	}
	defer session.Close()
	return session.Run(fmt.Sprintf("test -f %s", path)) == nil
}

func (r *VMReconciler) isProcessRunning(client *ssh.Client, pattern string) bool {
	session, err := client.NewSession()
	if err != nil {
		return false
	}
	defer session.Close()
	return session.Run(fmt.Sprintf("pgrep -f %q", pattern)) == nil
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
