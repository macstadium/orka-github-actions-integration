// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

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
	ProvisionRunner(ctx context.Context) error
}

type RunnerMessageProcessor struct {
	ctx                context.Context
	logger             *zap.SugaredLogger
	runnerManager      RunnerManagerInterface
	runnerProvisioner  RunnerProvisionerInterface
	runnerScaleSetName string
	canceledJobs       map[string]bool
}
