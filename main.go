package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/runners"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	provisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/validation"
)

var runnerScaleSetIDs = []int{}

func main() {
	envData := env.ParseEnv()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logging.SetupLogger(envData.LogLevel)
	logger := logging.Logger.Named("main")

	config, err := github.NewGitHubConfig(envData.GitHubURL)
	if err != nil {
		panic(err)
	}

	runnerName := envData.Runners[0].Name
	groupId := constants.DefaultRunnerGroupID
	if envData.Runners[0].Id != 0 {
		groupId = envData.Runners[0].Id
	}

	if len(validation.IsValidLabelValue(runnerName)) > 0 || len(validation.IsDNS1035Label(runnerName)) > 0 {
		panic(fmt.Sprintf("invalid runner name: %s. Runner name must consist of lower case alphanumeric characters or ' - ', start with an alphabetic character, end with an alphanumeric character, and may not be longer than 63 characters.", runnerName))
	}

	actionsClient, err := actions.NewActionsClient(ctx, envData, config)
	if err != nil {
		panic(err)
	}

	runnerScaleSet, err := actionsClient.GetRunnerScaleSet(ctx, groupId, runnerName)
	if err != nil {
		panic(err)
	}

	if runnerScaleSet != nil {
		err = actionsClient.DeleteRunnerScaleSet(ctx, runnerScaleSet.Id)
		if err != nil {
			panic(err)
		}
	}

	go func() {
		// Wait for termination signal
		<-ctx.Done()

		if ctx.Err() == context.Canceled {
			fmt.Println("Received termination signal. Performing cleanup...")

			for _, runnerScaleSetID := range runnerScaleSetIDs {
				err = actionsClient.DeleteRunnerScaleSet(context.TODO(), runnerScaleSetID)
				if err != nil {
					fmt.Printf("error while deleting runnerScaleSet %s", err.Error())
				}
			}

			os.Exit(0)
		}
	}()

	runnerScaleSet, err = actionsClient.CreateRunnerScaleSet(ctx, &types.RunnerScaleSet{
		Name:          runnerName,
		RunnerGroupId: groupId,
		Labels: []types.RunnerScaleSetLabel{
			{
				Name: runnerName,
				Type: "System",
			},
		},
		RunnerSetting: types.RunnerScaleSetSetting{
			Ephemeral:     true,
			DisableUpdate: true,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("unable to create runner %s, err: %s", runnerName, err.Error()))
	}

	runnerScaleSetIDs = append(runnerScaleSetIDs, runnerScaleSet.Id)

	orkaClient, err := orka.NewOrkaClient(envData, ctx)
	if err != nil {
		panic(fmt.Sprintf("unable to access Orka cluster. More info: %s", err.Error()))
	}

	run(ctx, actionsClient, orkaClient, runnerScaleSet, envData, logger)
}

func run(ctx context.Context, actionsClient *actions.ActionsClient, orkaClient *orka.OrkaClient, runnerScaleSet *types.RunnerScaleSet, envData *env.Data, logger *zap.SugaredLogger) {
	runnerManager, err := runners.NewRunnerManager(ctx, actionsClient, runnerScaleSet.Id)
	if err != nil {
		panic(err)
	}
	defer runnerManager.Close()

	runnerProvisioner := provisioner.NewRunnerProvisioner(runnerScaleSet, actionsClient, orkaClient, envData)

	runnerMessageProcessor := runners.NewRunnerMessageProcessor(ctx, runnerManager, runnerProvisioner, runnerScaleSet)

	if err = runnerMessageProcessor.StartProcessingMessages(); err != nil {
		logger.Errorf("failed to start processing messages for runnerScaleSet %s: %w", runnerScaleSet.Name, err.Error())
	}
}
