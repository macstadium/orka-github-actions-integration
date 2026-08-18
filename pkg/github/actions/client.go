// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"context"

	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
)

type ActionsService interface {
	GetRunnerScaleSet(ctx context.Context, runnerGroupId int, runnerScaleSetName string) (*types.RunnerScaleSet, error)
	CreateRunnerScaleSet(ctx context.Context, runnerScaleSet *types.RunnerScaleSet) (*types.RunnerScaleSet, error)
	DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error

	GetRunner(ctx context.Context, runnerName string) (*types.RunnerReference, error)
	CreateRunner(ctx context.Context, runnerScaleSetID int, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error)
	DeleteRunner(ctx context.Context, runnerID int) error

	CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*types.RunnerScaleSetSession, error)
	DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) error
	RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) (*types.RunnerScaleSetSession, error)

	AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error)
	GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*types.AcquirableJobList, error)

	GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64) (*types.RunnerScaleSetMessage, error)
	DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error
}
