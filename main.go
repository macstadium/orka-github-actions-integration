package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/runners"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/metrics"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	provisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/validation"
)

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

	existing, err := actionsClient.GetRunnerScaleSet(ctx, groupId, runnerName)
	if err != nil {
		panic(fmt.Sprintf("error checking for existing runner scale set: %s", err.Error()))
	}

	var runnerScaleSet *types.RunnerScaleSet
	if existing != nil && !envData.ManageRunnerScaleSets {
		logger.Infof("reusing existing runner scale set %s (id=%d)", existing.Name, existing.Id)
		runnerScaleSet = existing
	} else {
		if existing != nil {
			if err = actionsClient.DeleteRunnerScaleSet(ctx, existing.Id); err != nil {
				panic(fmt.Sprintf("error deleting existing runner scale set: %s", err.Error()))
			}
		}
		runnerScaleSet, err = createScaleSet(ctx, actionsClient, runnerName, groupId)
		if err != nil {
			panic(fmt.Sprintf("unable to create runner %s, err: %s", runnerName, err.Error()))
		}
		logger.Infof("created runner scale set %s (id=%d)", runnerScaleSet.Name, runnerScaleSet.Id)
	}

	if envData.EnableMetrics {
		metrics.Start(ctx, logger, envData, actionsClient, runnerName, groupId)
	}

	orkaClient, err := orka.NewOrkaClient(envData, ctx)
	if err != nil {
		panic(fmt.Sprintf("unable to access Orka cluster. More info: %s", err.Error()))
	}

	runnerManager, err := runners.NewRunnerManager(ctx, actionsClient, runnerScaleSet.Id)
	if errors.Is(err, runners.ErrActiveSession) {
		logger.Infof("scale set %s (id=%d) has a stale active session, deleting and recreating", runnerScaleSet.Name, runnerScaleSet.Id)
		if err = actionsClient.DeleteRunnerScaleSet(ctx, runnerScaleSet.Id); err != nil {
			panic(fmt.Sprintf("error deleting scale set with active session: %s", err.Error()))
		}
		runnerScaleSet, err = createScaleSet(ctx, actionsClient, runnerName, groupId)
		if err != nil {
			panic(fmt.Sprintf("error recreating scale set after active session conflict: %s", err.Error()))
		}
		logger.Infof("recreated scale set %s (id=%d)", runnerScaleSet.Name, runnerScaleSet.Id)
		runnerManager, err = runners.NewRunnerManager(ctx, actionsClient, runnerScaleSet.Id)
	}
	if err != nil {
		panic(err)
	}

	var closeOnce sync.Once
	closeManager := func() {
		closeOnce.Do(func() { runnerManager.Close() })
	}
	defer closeManager()

	go func() {
		<-ctx.Done()

		if ctx.Err() == context.Canceled {
			logger.Info("received termination signal, performing cleanup")

			closeManager()

			if envData.ManageRunnerScaleSets {
				if err := actionsClient.DeleteRunnerScaleSet(context.TODO(), runnerScaleSet.Id); err != nil {
					logger.Errorf("error deleting runner scale set on exit: %s", err.Error())
				}
			}

			os.Exit(0)
		}
	}()

	run(ctx, actionsClient, orkaClient, runnerScaleSet, runnerManager, envData, logger)
}

func createScaleSet(ctx context.Context, actionsClient *actions.ActionsClient, runnerName string, groupId int) (*types.RunnerScaleSet, error) {
	return actionsClient.CreateRunnerScaleSet(ctx, &types.RunnerScaleSet{
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
}

func run(ctx context.Context, actionsClient *actions.ActionsClient, orkaClient *orka.OrkaClient, runnerScaleSet *types.RunnerScaleSet, runnerManager *runners.RunnerManager, envData *env.Data, logger *zap.SugaredLogger) {
	runnerProvisioner := provisioner.NewRunnerProvisioner(runnerScaleSet, actionsClient, orkaClient, envData)

	vmTracker := runners.NewVMTracker(orkaClient, actionsClient, logger)
	go vmTracker.Start(ctx, envData.VMTrackerInterval)

	runnerMessageProcessor := runners.NewRunnerMessageProcessor(ctx, runnerManager, runnerProvisioner, vmTracker, runnerScaleSet)

	if err := runnerMessageProcessor.StartProcessingMessages(); err != nil && !errors.Is(err, context.Canceled) {
		logger.Errorf("failed to start processing messages for runnerScaleSet %s: %v", runnerScaleSet.Name, err)
	}
}
