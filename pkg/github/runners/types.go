package runners

import (
	"context"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/messagequeue"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"go.uber.org/zap"
)

type RunnerManagerInterface interface {
	ProcessMessages(ctx context.Context, handler func(msg *types.RunnerScaleSetMessage) error) error
	AcquireJobs(ctx context.Context, requestIds []int64) error
}

type RunnerManager struct {
	messageQueueManager messagequeue.MessageQueueManagerInterface
	logger              *zap.SugaredLogger

	lastMessageId  int64
	initialMessage *types.RunnerScaleSetMessage

	runnerScaleSetId int
	actionsClient    actions.ActionsService
}

type RunnerProvisionerInterface interface {
	ProvisionJITRunner(ctx context.Context, runnerName string, runnerCount int) error
	HandleJobStartedForRunner(ctx context.Context, runnerName, ownerName, repositoryName, jobWorkflowRef, jobDisplayName string, jobRequestId, workflowRunId int64)
}

type RunnerMessageProcessor struct {
	ctx                context.Context
	logger             *zap.SugaredLogger
	runnerManager      RunnerManagerInterface
	runnerProvisioner  RunnerProvisionerInterface
	settings           *RunnerScaleSettings
	currentRunnerCount int
}
