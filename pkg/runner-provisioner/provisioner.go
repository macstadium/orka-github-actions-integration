package provisioner

import (
	"context"
	"fmt"
	"strings"

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
}

var vmConfigToAgentType = map[string]string{
	"amd64": "x64",
	"arm64": "arm64",
}

var commands_template = []string{
	"echo 'Downloading Git Action Runner from https://github.com/actions/runner/releases/download/v$VERSION/actions-runner-osx-$CPU-$VERSION.tar.gz'",
	"mkdir -p /Users/$USERNAME/actions-runner",
	"curl -o /Users/$USERNAME/actions-runner/actions-runner.tar.gz -L https://github.com/actions/runner/releases/download/v$VERSION/actions-runner-osx-$CPU-$VERSION.tar.gz",
	"echo 'Git Action Runner download completed'",
	"echo 'Unarchiving Git Action Runner /Users/$USERNAME/actions-runner/actions-runner.tar.gz'",
	"cd /Users/$USERNAME/actions-runner",
	"tar xzf /Users/$USERNAME/actions-runner/actions-runner.tar.gz",
	"echo 'Git Action Runner unarchive completed'",
	"echo 'Starting Git Action Runner'",
	"/Users/$USERNAME/actions-runner/run.sh --jitconfig $JITCONFIG",
	"echo 'Git Action Runner exited'",
}

func (p *RunnerProvisioner) ProvisionRunner(ctx context.Context, runnerName string) error {
	vmConfig, err := p.orkaClient.GetVMConfig(ctx, p.envData.OrkaVMConfig)
	if err != nil {
		return err
	}

	runnerType := vmConfig.Type

	if runnerType == "" {
		image, err := p.orkaClient.GetImage(ctx, vmConfig.Image)
		if err != nil {
			return err
		}

		runnerType = image.Type
	}

	p.logger.Infof("found VM config %v", vmConfig)

	jitConfig, err := p.actionsClient.GenerateJITRunnerConfig(ctx, p.runnerScaleSet.Id, runnerName)
	if err != nil {
		return err
	}

	p.logger.Infof("deploying Orka VM with name %s", runnerName)
	vmResponse, err := p.orkaClient.DeployVM(ctx, runnerName, p.envData.OrkaVMConfig)
	if err != nil {
		return err
	}
	p.logger.Infof("deployed Orka VM with name %s", runnerName)

	defer p.DeprovisionRunner(ctx, runnerName)

	vmCommandExecutor := &orka.VMCommandExecutor{
		VMIP:         vmResponse.IP,
		VMPort:       *vmResponse.SSH,
		VMConfigName: p.envData.OrkaVMConfig,
		VMUsername:   p.envData.OrkaVMUsername,
		VMPassword:   p.envData.OrkaVMPassword,
	}

	return vmCommandExecutor.ExecuteCommands(buildCommands(jitConfig.EncodedJITConfig, vmConfigToAgentType[runnerType], p.envData.GitHubRunnerVersion, p.envData.OrkaVMUsername)...)
}

func (p *RunnerProvisioner) DeprovisionRunner(ctx context.Context, runnerName string) {
	p.logger.Infof("deleting Orka VM with name %s", runnerName)
	err := p.orkaClient.DeleteVM(ctx, runnerName)
	if err != nil {
		p.logger.Infof("error while deleting Orka VM %s. More information: %s", runnerName, err.Error())
	} else {
		p.logger.Infof("deleted Orka VM with name %s", runnerName)
	}
}

func buildCommands(jitConfig, cpu, version, username string) []string {
	commands := utils.Map(
		commands_template,
		func(cmd string) string {
			result := strings.ReplaceAll(cmd, "$JITCONFIG", jitConfig)
			result = strings.ReplaceAll(result, "$VERSION", version)
			result = strings.ReplaceAll(result, "$CPU", cpu)
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
