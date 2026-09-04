package provisioner

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	"github.com/macstadium/orka-github-actions-integration/pkg/utils"
	"go.uber.org/zap"
)

type RunnerProvisioner struct {
	runnerScaleSet *types.RunnerScaleSet
	actionsClient  actions.ActionsService
	envData        *env.Data

	orkaClient orka.OrkaService
	logger     *zap.SugaredLogger

	mu sync.Mutex
}

const (
	SentinelSetupComplete = "/tmp/orka-runner-setup-complete"
	SentinelRunComplete   = "/tmp/orka-runner-run-complete"
)

var commands_template = []string{
	"set -e",
	"echo \"Downloading Git Action Runner from https://github.com/actions/runner/releases/download/v$VERSION/actions-runner-osx-$(uname -m | sed 's/86_//')-$VERSION.tar.gz\"",
	"mkdir -p /Users/$USERNAME/actions-runner",
	"curl -fSL --retry 10 --retry-delay 5 --retry-all-errors -o /Users/$USERNAME/actions-runner/actions-runner.tar.gz https://github.com/actions/runner/releases/download/v$VERSION/actions-runner-osx-$(uname -m | sed 's/86_//')-$VERSION.tar.gz",
	"echo 'Git Action Runner download completed'",
	"echo 'Unarchiving Git Action Runner /Users/$USERNAME/actions-runner/actions-runner.tar.gz'",
	"cd /Users/$USERNAME/actions-runner",
	"tar xzf /Users/$USERNAME/actions-runner/actions-runner.tar.gz",
	"echo 'Git Action Runner unarchive completed'",
	"touch " + SentinelSetupComplete,
	"echo 'Starting Git Action Runner'",
	"/Users/$USERNAME/actions-runner/run.sh --jitconfig $JITCONFIG",
	"touch " + SentinelRunComplete,
	"echo 'Git Action Runner exited'",
}

func (p *RunnerProvisioner) ProvisionRunner(ctx context.Context) (*orka.VMCommandExecutor, []string, error) {
	p.logger.Infof("deploying Orka VM with prefix %s", p.runnerScaleSet.Name)
	vmResponse, err := p.orkaClient.DeployVM(ctx, p.runnerScaleSet.Name, p.envData.OrkaVMConfig)
	if err != nil {
		p.logger.Errorf("failed to deploy Orka VM: %v", err)
		return nil, nil, err
	}

	runnerName := vmResponse.Name
	p.logger.Infof("deployed Orka VM with name %s", runnerName)

	provisioningSucceeded := false

	defer func() {
		if !provisioningSucceeded {
			p.logger.Warnf("provisioning failed, cleaning up resources for VM %s", runnerName)
			p.cleanupResources(context.WithoutCancel(ctx), runnerName)
		}
	}()

	vmIP, err := p.getRealVMIP(vmResponse.IP)
	if err != nil {
		p.logger.Errorf("failed to get real VM IP for %s: %v", runnerName, err)
		return nil, nil, err
	}

	// Emulators are deployed before the JIT runner config because a cold deploy blocks on an
	// sdkmanager pull that can run for minutes, and a JIT config created first would sit aging
	// through that wait.
	emulators, err := p.deployEmulators(ctx, runnerName)
	if err != nil {
		p.logger.Errorf("failed to deploy emulators for %s: %v", runnerName, err)
		return nil, nil, err
	}

	p.logger.Infof("creating runner config for name %s", runnerName)
	jitConfig, err := p.createRunner(ctx, runnerName)
	if err != nil {
		p.logger.Errorf("failed to create runner config for %s: %v", runnerName, err)
		return nil, nil, err
	}
	p.logger.Infof("created runner config with name %s", runnerName)

	vmCommandExecutor := &orka.VMCommandExecutor{
		VMIP:       vmIP,
		VMPort:     *vmResponse.SSH,
		VMName:     runnerName,
		VMUsername: p.envData.OrkaVMUsername,
		VMPassword: p.envData.OrkaVMPassword,
		Logger:     p.logger,
	}

	commands := buildCommands(jitConfig.EncodedJITConfig, p.envData.GitHubRunnerVersion, p.envData.OrkaVMUsername, emulators)

	provisioningSucceeded = true

	return vmCommandExecutor, commands, nil
}

// provisionedEmulator pairs a deployed emulator with the emulator config that produced it. The
// deploy response resolves platform, image type, and device profile from the config but does not
// echo the config name back, so it is carried alongside.
type provisionedEmulator struct {
	config   string
	emulator *orka.OrkaEmulatorResponseModel
}

// emulatorName is the deterministic name of the nth emulator (1-indexed) paired with a VM. Deriving
// it from the VM name means cleanup and reconciliation never have to parse CLI output to discover
// what to delete, and it covers the case where a deploy times out: the CLI abandons the emulator
// record instead of removing it, so we have to be able to name it ourselves.
func emulatorName(vmName string, index int) string {
	return fmt.Sprintf("%s-emu-%d", vmName, index+1)
}

// deployEmulators brings up one emulator per configured emulator config, concurrently. Concurrency
// is safe because the operator's port allocator holds a pending reservation for each console port
// until the pod is listable; deploying in sequence would instead cost N times the deploy latency.
func (p *RunnerProvisioner) deployEmulators(ctx context.Context, vmName string) ([]provisionedEmulator, error) {
	configs := p.envData.OrkaEmulatorConfigs
	if len(configs) == 0 {
		return nil, nil
	}

	p.logger.Infof("deploying %d Android emulator(s) for VM %s from config(s) %s", len(configs), vmName, strings.Join(configs, ", "))

	var wg sync.WaitGroup
	provisioned := make([]provisionedEmulator, len(configs))
	failures := make([]error, len(configs))

	for i, config := range configs {
		wg.Add(1)

		go func(index int, emulatorConfig string) {
			defer wg.Done()

			name := emulatorName(vmName, index)

			emulator, err := p.orkaClient.DeployEmulator(ctx, name, vmName, emulatorConfig)
			if err != nil {
				failures[index] = fmt.Errorf("emulator %s from config %s: %w", name, emulatorConfig, err)
				return
			}

			// A running emulator without a relay address is unusable: the relay is the only way a
			// job inside the VM can reach it, so treat this as a provisioning failure rather than
			// exporting an empty address the workflow cannot act on.
			if !emulator.HasRelay() {
				failures[index] = fmt.Errorf("emulator %s from config %s reported status %s with no relay address", name, emulatorConfig, emulator.Status)
				return
			}

			provisioned[index] = provisionedEmulator{config: emulatorConfig, emulator: emulator}
		}(i, config)
	}

	wg.Wait()

	if err := errors.Join(failures...); err != nil {
		return nil, err
	}

	for _, entry := range provisioned {
		p.logger.Infof("emulator %s ready for VM %s at %s (platform %s, image type %s)", entry.emulator.Name, vmName, entry.emulator.ADBTarget(), entry.emulator.Platform, entry.emulator.ImageType)
	}

	return provisioned, nil
}

// deleteEmulators removes the emulators paired with a VM ahead of the VM itself. The operator
// garbage-collects them anyway through the VM's owner reference, so this is best effort: it frees
// the node's console and ADB ports promptly and keeps the logs explicit, but a failure here must
// never stop the VM from being deleted.
func (p *RunnerProvisioner) deleteEmulators(ctx context.Context, vmName string) {
	if !p.envData.EmulatorsEnabled() {
		return
	}

	names := make([]string, 0, len(p.envData.OrkaEmulatorConfigs))
	for i := range p.envData.OrkaEmulatorConfigs {
		names = append(names, emulatorName(vmName, i))
	}

	p.logger.Infof("deleting emulator(s) %s for VM %s", strings.Join(names, ", "), vmName)

	if err := p.orkaClient.DeleteEmulator(ctx, names...); err != nil {
		p.logger.Warnf("failed to delete emulator(s) for VM %s, falling back to owner-reference cleanup when the VM is deleted: %v", vmName, err)
		return
	}

	p.logger.Infof("deleted emulator(s) for VM %s", vmName)
}

func (p *RunnerProvisioner) CleanupResources(ctx context.Context, runnerName string) {
	p.logger.Infof("starting resource cleanup for %s", runnerName)
	p.cleanupResources(ctx, runnerName)
	p.logger.Infof("resource cleanup completed for %s", runnerName)
}

func (p *RunnerProvisioner) getRealVMIP(vmIP string) (string, error) {
	if !p.envData.OrkaEnableNodeIPMapping {
		return vmIP, nil
	}

	if p.envData.OrkaNodeIPMapping[vmIP] == "" {
		return "", fmt.Errorf("unable to retrieve VM IP from the provided node IP mapping")
	}

	return p.envData.OrkaNodeIPMapping[vmIP], nil
}

func (p *RunnerProvisioner) cleanupResources(ctx context.Context, runnerName string) {
	p.logger.Infof("starting resource cleanup for %s", runnerName)

	for {
		err := p.ensureRunnerDeregistered(ctx, runnerName)
		if err != nil {
			if strings.Contains(err.Error(), "is currently running a job and cannot be deleted") {
				p.logger.Infof("runner %s is currently running a job, repeating deletion logic", runnerName)
				continue
			}

			p.logger.Errorf("failed to delete runner %s (timeout or other error: %v). VM will not be deleted.", runnerName, err)
			return
		}

		break
	}

	p.deleteEmulators(ctx, runnerName)
	p.deleteVM(ctx, runnerName)
}

func (p *RunnerProvisioner) deleteVM(ctx context.Context, runnerName string) {
	p.logger.Infof("initiating deletion of Orka VM %s", runnerName)

	attempts := 0
	operation := func() error {
		attempts++
		err := p.orkaClient.DeleteVM(ctx, runnerName)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				p.logger.Warnf("Orka VM %s not found (it may have already been deleted)", runnerName)
				return nil
			}
			p.logger.Warnf("attempt %d: failed to delete Orka VM %s: %v", attempts, runnerName, err)
			return err
		}
		return nil
	}

	err := backoff.Retry(operation, backoff.NewExponentialBackOff())
	if err != nil {
		p.logger.Errorf("error while deleting Orka VM %s. More information: %s", runnerName, err.Error())
	} else {
		p.logger.Infof("successfully deleted Orka VM %s", runnerName)
	}
}

func (p *RunnerProvisioner) ensureRunnerDeregistered(ctx context.Context, runnerName string) error {
	p.logger.Infof("waiting for runner %s to de-register from GitHub", runnerName)

	timeoutCtx, cancel := context.WithTimeout(ctx, p.envData.RunnerDeregistrationTimeout)
	defer cancel()

	ticker := time.NewTicker(p.envData.RunnerDeregistrationPollInterval)
	defer ticker.Stop()

	if runner, err := p.actionsClient.GetRunner(ctx, runnerName); err == nil && runner == nil {
		p.logger.Infof("runner %s has cleanly de-registered from GitHub", runnerName)
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			p.logger.Warnf("context cancelled while waiting for runner %s to deregister: %v", runnerName, ctx.Err())
			return ctx.Err()

		case <-timeoutCtx.Done():
			p.logger.Warnf("runner %s did not de-register within %v, force-deleting from GitHub",
				runnerName, p.envData.RunnerDeregistrationTimeout)
			return p.forceDeleteRunner(ctx, runnerName)

		case <-ticker.C:
			runner, err := p.actionsClient.GetRunner(ctx, runnerName)
			if err != nil {
				p.logger.Warnf("error checking registration status for runner %s: %v", runnerName, err)
				continue
			}

			if runner == nil {
				p.logger.Infof("runner %s has cleanly de-registered from GitHub", runnerName)
				return nil
			}
		}
	}
}

func (p *RunnerProvisioner) forceDeleteRunner(ctx context.Context, runnerName string) error {
	runner, err := p.actionsClient.GetRunner(ctx, runnerName)
	if err != nil {
		p.logger.Errorf("failed to fetch runner %s for force-deletion: %v", runnerName, err)
		return err
	}

	if runner == nil {
		p.logger.Infof("runner %s already de-registered, no force-deletion needed", runnerName)
		return nil
	}

	err = p.actionsClient.DeleteRunner(ctx, runner.Id)
	if err != nil {
		p.logger.Errorf("failed to force-delete runner %s (ID: %d) from GitHub: %v", runnerName, runner.Id, err)
		return err
	}

	p.logger.Infof("successfully force-deleted runner %s (ID: %d) from GitHub", runnerName, runner.Id)
	return nil
}

func (p *RunnerProvisioner) createRunner(ctx context.Context, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	p.logger.Debugf("waiting for lock to create runner %s", runnerName)
	p.mu.Lock()
	p.logger.Debugf("acquired lock for runner %s", runnerName)

	defer func() {
		p.mu.Unlock()
		p.logger.Debugf("released lock for runner %s", runnerName)
	}()

	jitConfig, err := p.actionsClient.CreateRunner(ctx, p.runnerScaleSet.Id, runnerName)
	if err != nil {
		return nil, err
	}

	return jitConfig, nil
}

func buildCommands(jitConfig, version, username string, emulators []provisionedEmulator) []string {
	commands := utils.Map(
		commands_template,
		func(cmd string) string {
			result := strings.ReplaceAll(cmd, "$JITCONFIG", jitConfig)
			result = strings.ReplaceAll(result, "$VERSION", version)
			result = strings.ReplaceAll(result, "$USERNAME", username)

			return result
		},
	)

	// Exports go ahead of the runner so the environment is in place when run.sh starts. The runner
	// passes its own environment through to job steps, which is the only channel a job has for
	// learning its emulator's address: it cannot reach the Orka control plane from inside the VM.
	return append(buildEmulatorExports(emulators), commands...)
}

// buildEmulatorExports renders the emulator environment as export statements. These are generated
// as discrete lines rather than folded into commands_template's placeholder substitution so that
// every value is single-quoted: the values come from the cluster, not from us, and must not be able
// to break out of the command stream.
func buildEmulatorExports(emulators []provisionedEmulator) []string {
	if len(emulators) == 0 {
		return nil
	}

	exports := []string{shellExport("ORKA_EMULATOR_COUNT", strconv.Itoa(len(emulators)))}
	targets := make([]string, 0, len(emulators))

	for i, entry := range emulators {
		emulator := entry.emulator
		prefix := fmt.Sprintf("ORKA_EMULATOR_%d", i+1)

		// deployEmulators rejects any emulator without a relay, so these are safe to read.
		host := *emulator.RelayIP
		port := strconv.Itoa(*emulator.RelayPort)
		target := emulator.ADBTarget()
		targets = append(targets, target)

		exports = append(exports,
			shellExport(prefix+"_NAME", emulator.Name),
			shellExport(prefix+"_CONFIG", entry.config),
			shellExport(prefix+"_ADB_HOST", host),
			shellExport(prefix+"_ADB_PORT", port),
			shellExport(prefix+"_ADB", target),
			shellExport(prefix+"_PLATFORM", emulator.Platform),
			shellExport(prefix+"_IMAGE_TYPE", emulator.ImageType),
			shellExport(prefix+"_DEVICE_PROFILE", derefOrEmpty(emulator.DeviceProfile)),
		)
	}

	// Unprefixed aliases for the common single-emulator case, so a workflow that only ever uses one
	// device does not have to carry an index.
	first := emulators[0].emulator
	exports = append(exports,
		shellExport("ORKA_EMULATOR_ADB_HOST", *first.RelayIP),
		shellExport("ORKA_EMULATOR_ADB_PORT", strconv.Itoa(*first.RelayPort)),
		shellExport("ORKA_EMULATOR_ADB", targets[0]),
		shellExport("ORKA_EMULATOR_ADB_TARGETS", strings.Join(targets, ",")),
	)

	return exports
}

func shellExport(name, value string) string {
	return fmt.Sprintf("export %s=%s", name, singleQuote(value))
}

// singleQuote wraps a value for safe use in a POSIX shell, closing and reopening the quote around
// any embedded single quote.
func singleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func derefOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func NewRunnerProvisioner(runnerScaleSet *types.RunnerScaleSet, actionsClient actions.ActionsService, orkaClient orka.OrkaService, envData *env.Data) *RunnerProvisioner {
	return &RunnerProvisioner{
		runnerScaleSet: runnerScaleSet,
		actionsClient:  actionsClient,
		envData:        envData,
		orkaClient:     orkaClient,
		logger:         logging.Logger.Named(fmt.Sprintf("runner-provisioner-%d", runnerScaleSet.Id)),
	}
}
