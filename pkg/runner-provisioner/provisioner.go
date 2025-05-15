package provisioner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	"github.com/macstadium/orka-github-actions-integration/pkg/utils"
	"go.uber.org/zap"
)

type CancelContext struct {
	context context.Context
	cancel  context.CancelFunc
}

type RunnerProvisioner struct {
	runnerScaleSet *types.RunnerScaleSet
	actionsClient  actions.ActionsService
	envData        *env.Data

	orkaClient orka.OrkaService
	logger     *zap.SugaredLogger

	mu                sync.Mutex
	runnerToJitConfig map[string]*types.RunnerScaleSetJitRunnerConfig

	cancelContextLock     sync.Mutex
	runnerToCancelContext map[string]*CancelContext
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

	cancelContext := p.createCancelContext(ctx, runnerName)

	vmCommandExecutor := &orka.VMCommandExecutor{
		VMIP:       vmIP,
		VMPort:     *vmResponse.SSH,
		VMName:     runnerName,
		VMUsername: p.envData.OrkaVMUsername,
		VMPassword: p.envData.OrkaVMPassword,
		Context:    cancelContext.context,
	}

	err = vmCommandExecutor.ExecuteCommands(buildCommands(jitConfig.EncodedJITConfig, p.envData.GitHubRunnerVersion, p.envData.OrkaVMUsername)...)
	if err != nil {
		return err
	}

	delete(p.runnerToJitConfig, runnerName)

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

func (p *RunnerProvisioner) DeprovisionRunner(ctx context.Context, runnerName string) {
	p.logger.Infof("deleting runner with name %s", runnerName)
	runner, err := p.actionsClient.GetRunner(ctx, runnerName)
	if err != nil || runner == nil {
		p.logger.Infof("unable to find runner with name %s", runnerName)
	} else {
		err = p.actionsClient.DeleteRunner(ctx, runner.Id)
		if err != nil {
			p.logger.Infof("error while deleting runner %s. More information: %s", runnerName, err.Error())
		} else {
			p.logger.Infof("deleted runner with name %s", runnerName)
		}
	}

	p.deleteVM(ctx, runnerName)

	delete(p.runnerToJitConfig, runnerName)

	if p.runnerToCancelContext[runnerName] != nil {
		p.runnerToCancelContext[runnerName].cancel()
		delete(p.runnerToCancelContext, runnerName)
	}
}

func (p *RunnerProvisioner) deleteVM(ctx context.Context, runnerName string) {
	p.logger.Infof("deleting Orka VM with name %s", runnerName)
	err := p.orkaClient.DeleteVM(ctx, runnerName)
	if err != nil {
		p.logger.Infof("error while deleting Orka VM %s. More information: %s", runnerName, err.Error())
	} else {
		p.logger.Infof("deleted Orka VM with name %s", runnerName)
	}
}

func (p *RunnerProvisioner) createRunner(ctx context.Context, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.runnerToJitConfig[runnerName] != nil {
		return p.runnerToJitConfig[runnerName], nil
	}

	jitConfig, err := p.actionsClient.CreateRunner(ctx, p.runnerScaleSet.Id, runnerName)
	if err != nil {
		return nil, err
	}

	p.runnerToJitConfig[runnerName] = jitConfig

	return jitConfig, nil
}

func (p *RunnerProvisioner) createCancelContext(ctx context.Context, runnerName string) *CancelContext {
	p.cancelContextLock.Lock()
	defer p.cancelContextLock.Unlock()

	if p.runnerToCancelContext[runnerName] == nil {
		cancelContext, cancel := context.WithCancel(ctx)
		p.runnerToCancelContext[runnerName] = &CancelContext{
			context: cancelContext,
			cancel:  cancel,
		}
	}

	return p.runnerToCancelContext[runnerName]
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
		runnerScaleSet:        runnerScaleSet,
		actionsClient:         actionsClient,
		envData:               envData,
		orkaClient:            orkaClient,
		logger:                logging.Logger.Named(fmt.Sprintf("runner-provisioner-%d", runnerScaleSet.Id)),
		runnerToJitConfig:     make(map[string]*types.RunnerScaleSetJitRunnerConfig),
		runnerToCancelContext: make(map[string]*CancelContext),
	}
}
