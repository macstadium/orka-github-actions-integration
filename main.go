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
)

type RunnerProvisioner struct {
	runnerScaleSet *types.RunnerScaleSet
	client         *actions.ActionsClient
	envData        *env.Data
}

func (h *RunnerProvisioner) ProvisionJITRunner(ctx context.Context, runnerName string, runnerCount int) error {
	return nil
}

func (h RunnerProvisioner) HandleJobStartedForRunner(ctx context.Context, runnerName, ownerName, repositoryName, jobWorkflowRef, jobDisplayName string, jobRequestId, workflowRunId int64) {
}

var runnerScaleSetIDs = []int{}

func main() {
	envData := env.ParseEnv()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logging.SetupLogger(envData.LogLevel)

	config, err := github.NewGitHubConfig(envData.GitHubURL)
	if err != nil {
		panic(err)
	}

	actionsClient, err := actions.NewActionsClient(ctx, envData, config)
	if err != nil {
		panic(err)
	}

	runnerName := envData.Runners[0].Name

	runnerScaleSet, err := actionsClient.GetRunnerScaleSet(ctx, constants.DefaultRunnerGroupID, runnerName)
	if err != nil {
		panic(err)
	}

	if runnerScaleSet == nil {
		runnerScaleSet, err = actionsClient.CreateRunnerScaleSet(ctx, &types.RunnerScaleSet{
			Name:          runnerName,
			RunnerGroupId: constants.DefaultRunnerGroupID,
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
	}

	runnerScaleSetIDs = append(runnerScaleSetIDs, runnerScaleSet.Id)

	// Start a goroutine to handle cleanup tasks
	// TODO: This works for the ctrl + C case
	// It doesn't work when the app crashes or when the app exits normally
	go func() {
		// Wait for termination signal
		<-ctx.Done()

		if ctx.Err() == context.Canceled {
			fmt.Println("Received termination signal. Performing cleanup...")

			for _, runnerScaleSetID := range runnerScaleSetIDs {
				err = actionsClient.DeleteRunnerScaleSet(context.TODO(), runnerScaleSetID)
			}

			os.Exit(0)
		}
	}()

	run(ctx, actionsClient, runnerScaleSet, envData)
}

func run(ctx context.Context, actionsClient *actions.ActionsClient, runnerScaleSet *types.RunnerScaleSet, envData *env.Data) {
	runnerManager, err := runners.NewRunnerManager(ctx, actionsClient, runnerScaleSet.Id)
	if err != nil {
		panic(err)
	}
	defer runnerManager.Close()

	runnerProvisioner := &RunnerProvisioner{runnerScaleSet: runnerScaleSet, client: actionsClient, envData: envData}

	runnerMessageProcessor := runners.NewRunnerMessageProcessor(ctx, runnerManager, runnerProvisioner, &runners.RunnerScaleSettings{
		RunnerName: runnerScaleSet.Name,
		MaxRunners: 10,
		MinRunners: 0,
	})

	if err = runnerMessageProcessor.StartProcessingMessages(); err != nil {
		panic(fmt.Errorf("failed to start processisng messages: %w", err))
	}
}
