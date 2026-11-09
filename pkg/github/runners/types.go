// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package runners

import (
	"context"
	"sync"
	"sync/atomic"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/messagequeue"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/metrics"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
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
	ProvisionRunner(ctx context.Context) (*orka.VMCommandExecutor, []string, error)
	CleanupResources(ctx context.Context, runnerName string)
}

// provisionRecorder is the slice of *metrics.Metrics the processor records into. A nil
// *metrics.Metrics satisfies it and records nothing.
type provisionRecorder interface {
	IncRunnerProvisionExhausted()
}

var _ provisionRecorder = (*metrics.Metrics)(nil)

type RunnerMessageProcessor struct {
	ctx                context.Context
	logger             *zap.SugaredLogger
	runnerManager      RunnerManagerInterface
	runnerProvisioner  RunnerProvisionerInterface
	vmTracker          *VMTracker
	metrics            provisionRecorder
	runnerScaleSetName string

	// upstreamCanceledJobsMutex guards both maps below, so marking a job canceled and cancelling
	// its in-flight provisioning happen together.
	upstreamCanceledJobs      map[jobIdentity]bool
	provisionCancels          map[jobIdentity]context.CancelFunc
	upstreamCanceledJobsMutex sync.RWMutex

	runnerContextCancels      map[string]context.CancelFunc
	runnerContextCancelsMutex sync.Mutex
	provisioningInflight      atomic.Int32

	// newProvisionBackoff produces the wait between provisioning attempts. Tests shorten it.
	newProvisionBackoff func() backoff.BackOff
}
