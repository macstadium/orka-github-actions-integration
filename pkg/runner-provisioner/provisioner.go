package provisioner

import (
	"context"
	"fmt"
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

var commands_template = []string{
	"set -e",
	"echo \"Downloading Git Action Runner from https://github.com/actions/runner/releases/download/v$VERSION/actions-runner-osx-$(uname -m | sed 's/86_//')-$VERSION.tar.gz\"",
	"mkdir -p /Users/$USERNAME/actions-runner",
	"curl -o /Users/$USERNAME/actions-runner/actions-runner.tar.gz -L https://github.com/actions/runner/releases/download/v$VERSION/actions-runner-osx-$(uname -m | sed 's/86_//')-$VERSION.tar.gz",
	"echo 'Git Action Runner download completed'",
	"echo 'Unarchiving Git Action Runner /Users/$USERNAME/actions-runner/actions-runner.tar.gz'",
	"cd /Users/$USERNAME/actions-runner",
	"tar xzf /Users/$USERNAME/actions-runner/actions-runner.tar.gz",
	"echo 'Git Action Runner unarchive completed'",
	"echo 'Starting Git Action Runner'",
	"/Users/$USERNAME/actions-runner/run.sh --jitconfig $JITCONFIG",
	"echo 'Git Action Runner exited'",
}

func (p *RunnerProvisioner) ProvisionRunner(ctx context.Context) (*orka.VMCommandExecutor, []string, func(error), error) {
	p.logger.Infof("deploying Orka VM with prefix %s", p.runnerScaleSet.Name)
	vmResponse, err := p.orkaClient.DeployVM(ctx, p.runnerScaleSet.Name, p.envData.OrkaVMConfig)
	if err != nil {
		p.logger.Errorf("failed to deploy Orka VM: %v", err)
		return nil, nil, nil, err
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
		return nil, nil, nil, err
	}

	p.logger.Infof("creating runner config for name %s", runnerName)
	jitConfig, err := p.createRunner(ctx, runnerName)
	if err != nil {
		p.logger.Errorf("failed to create runner config for %s: %v", runnerName, err)
		return nil, nil, nil, err
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

	commands := buildCommands(jitConfig.EncodedJITConfig, p.envData.GitHubRunnerVersion, p.envData.OrkaVMUsername)

	cleanup := func(execErr error) {
		if execErr != nil {
			if ctx.Err() != nil {
				p.logger.Warnf("context cancelled/timed out, triggering cleanup for %s", runnerName)
			} else {
				p.logger.Errorf("execution failed, triggering cleanup for %s: %v", runnerName, execErr)
			}
		} else {
			p.logger.Infof("execution completed normally, triggering cleanup for %s", runnerName)
		}
		p.cleanupResources(context.WithoutCancel(ctx), runnerName)
	}

	provisioningSucceeded = true

	return vmCommandExecutor, commands, cleanup, nil
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

func buildCommands(jitConfig, version, username string) []string {
	commands := utils.Map(
		commands_template,
		func(cmd string) string {
			result := strings.ReplaceAll(cmd, "$JITCONFIG", jitConfig)
			result = strings.ReplaceAll(result, "$VERSION", version)
			result = strings.ReplaceAll(result, "$USERNAME", username)

			return result
		},
	)
	return commands
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
