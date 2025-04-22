// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package messagequeue

import (
	"context"
	"io"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"go.uber.org/zap"
)

type MessageQueueManagerInterface interface {
	ReceiveNextMessage(ctx context.Context, lastMessageId int64) (*types.RunnerScaleSetMessage, error)
	DeleteMessage(ctx context.Context, messageId int64) error
	AcquireJobs(ctx context.Context, requestIds []int64) ([]int64, error)
	io.Closer
}

type MessageQueueManager struct {
	client  actions.ActionsService
	logger  *zap.SugaredLogger
	session *types.RunnerScaleSetSession
}
