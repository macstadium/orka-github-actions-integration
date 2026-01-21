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

func (p *RunnerProvisioner) ProvisionRunner(ctx context.Context) error {
	p.logger.Infof("deploying Orka VM with prefix  %s", p.runnerScaleSet.Name)
	vmResponse, err := p.orkaClient.DeployVM(ctx, p.runnerScaleSet.Name, p.envData.OrkaVMConfig)
	if err != nil {
		return err
	}

	runnerName := vmResponse.Name
	p.logger.Infof("deployed Orka VM with name %s", runnerName)

	defer p.deleteVM(ctx, runnerName)

	vmIP, err := p.getRealVMIP(vmResponse.IP)
	if err != nil {
		return err
	}

	p.logger.Infof("creating runner with name %s", runnerName)
	jitConfig, err := p.createRunner(ctx, runnerName)
	if err != nil {
		return err
	}
	p.logger.Infof("created runner with name %s", runnerName)

	vmCommandExecutor := &orka.VMCommandExecutor{
		VMIP:       vmIP,
		VMPort:     *vmResponse.SSH,
		VMName:     runnerName,
		VMUsername: p.envData.OrkaVMUsername,
		VMPassword: p.envData.OrkaVMPassword,
	}

	err = vmCommandExecutor.ExecuteCommands(buildCommands(jitConfig.EncodedJITConfig, p.envData.GitHubRunnerVersion, p.envData.OrkaVMUsername)...)
	if err != nil {
		return err
	}

	return nil
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

func (p *RunnerProvisioner) deleteVM(ctx context.Context, runnerName string) {
	p.logger.Infof("deleting Orka VM with name %s", runnerName)
	operation := func() error {
		err := p.orkaClient.DeleteVM(ctx, runnerName)
		if err != nil && strings.Contains(err.Error(), "not found") {
			p.logger.Warnf("vm not found for %s", runnerName)
			return nil
		}
		return err
	}
	err := backoff.Retry(operation, backoff.NewExponentialBackOff())
	if err != nil {
		p.logger.Infof("error while deleting Orka VM %s. More information: %s", runnerName, err.Error())
	} else {
		p.logger.Infof("deleted Orka VM with name %s", runnerName)
	}

	p.ensureRunnerDeregistered(ctx, runnerName)
}

func (p *RunnerProvisioner) ensureRunnerDeregistered(ctx context.Context, runnerName string) {
	p.logger.Infof("waiting for runner %s to de-register from GitHub", runnerName)

	timeoutCtx, cancel := context.WithTimeout(ctx, p.envData.RunnerDeregistrationTimeout)
	defer cancel()

	ticker := time.NewTicker(p.envData.RunnerDeregistrationPollInterval)
	defer ticker.Stop()

	if runner, err := p.actionsClient.GetRunner(ctx, runnerName); err == nil && runner == nil {
		p.logger.Infof("runner %s has de-registered from GitHub", runnerName)
		return
	}

	for {
		select {
		case <-timeoutCtx.Done():
			p.logger.Warnf("runner %s did not de-register within %v, force-deleting from GitHub",
				runnerName, p.envData.RunnerDeregistrationTimeout)
			p.forceDeleteRunner(ctx, runnerName)
			return

		case <-ticker.C:
			runner, err := p.actionsClient.GetRunner(ctx, runnerName)
			if err != nil {
				p.logger.Warnf("error checking runner %s registration status: %s", runnerName, err.Error())
				continue
			}

			if runner == nil {
				p.logger.Infof("runner %s has de-registered from GitHub", runnerName)
				return
			}
		}
	}
}

func (p *RunnerProvisioner) forceDeleteRunner(ctx context.Context, runnerName string) {
	runner, err := p.actionsClient.GetRunner(ctx, runnerName)
	if err != nil {
		p.logger.Errorf("error getting runner %s for force-deletion: %s", runnerName, err.Error())
		return
	}

	if runner == nil {
		p.logger.Infof("runner %s already de-registered, no force-deletion needed", runnerName)
		return
	}

	err = p.actionsClient.DeleteRunner(ctx, runner.Id)
	if err != nil {
		p.logger.Errorf("error force-deleting runner %s (ID: %d) from GitHub: %s", runnerName, runner.Id, err.Error())
		return
	}

	p.logger.Infof("successfully force-deleted runner %s (ID: %d) from GitHub", runnerName, runner.Id)
}

func (p *RunnerProvisioner) createRunner(ctx context.Context, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

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
