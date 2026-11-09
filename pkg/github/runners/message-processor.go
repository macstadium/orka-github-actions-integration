// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package runners

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/metrics"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	"golang.org/x/crypto/ssh"
)

const (
	cancelledStatus = "canceled"
	ignoredStatus   = "ignored"
	abandonedStatus = "abandoned"
)

type jobIdentity struct {
	jobId           string
	runnerRequestId int64
}

func newJobIdentity(jobId string, runnerRequestId int64) jobIdentity {
	return jobIdentity{jobId: jobId, runnerRequestId: runnerRequestId}
}

func newRecoveryJobIdentity() jobIdentity {
	return jobIdentity{jobId: "recovery-" + uuid.NewString()}
}

func (k jobIdentity) String() string {
	return fmt.Sprintf("jobId=%s runnerRequestId=%d", k.jobId, k.runnerRequestId)
}

// maxProvisionAttempts bounds how many times a job's runner is provisioned before giving up.
const maxProvisionAttempts = 5

// defaultProvisionBackoff waits 15s, 30s, 60s, then 120s (with jitter) between attempts, so a
// transient failure clears quickly while a node that stays full is not hammered.
func defaultProvisionBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 15 * time.Second
	b.Multiplier = 2
	b.MaxInterval = 2 * time.Minute
	// The attempt cap bounds the retries, not elapsed time.
	b.MaxElapsedTime = 0
	b.Reset()

	return b
}

// NewRunnerMessageProcessor builds a processor. m may be nil, in which case nothing is recorded.
func NewRunnerMessageProcessor(ctx context.Context, runnerManager RunnerManagerInterface, runnerProvisioner RunnerProvisionerInterface, vmTracker *VMTracker, runnerScaleSet *types.RunnerScaleSet, m *metrics.Metrics) *RunnerMessageProcessor {
	return &RunnerMessageProcessor{
		ctx:                       ctx,
		runnerManager:             runnerManager,
		runnerProvisioner:         runnerProvisioner,
		logger:                    logging.Logger.Named(fmt.Sprintf("runner-message-processor-%d", runnerScaleSet.Id)),
		runnerScaleSetName:        runnerScaleSet.Name,
		upstreamCanceledJobs:      map[jobIdentity]bool{},
		provisionCancels:          map[jobIdentity]context.CancelFunc{},
		upstreamCanceledJobsMutex: sync.RWMutex{},
		runnerContextCancels:      map[string]context.CancelFunc{},
		runnerContextCancelsMutex: sync.Mutex{},
		vmTracker:                 vmTracker,
		metrics:                   m,
		newProvisionBackoff:       defaultProvisionBackoff,
	}
}

func (p *RunnerMessageProcessor) StartProcessingMessages() error {
	for {
		p.logger.Infof("waiting for message for runner %s...", p.runnerScaleSetName)
		select {
		case <-p.ctx.Done():
			p.logger.Infof("message processing service is stopped for runner %s", p.runnerScaleSetName)
			return nil
		default:
			err := p.runnerManager.ProcessMessages(p.ctx, p.processRunnerMessage)
			if err != nil {
				return fmt.Errorf("could not get and process message. %w", err)
			}
		}
	}
}

func (p *RunnerMessageProcessor) processRunnerMessage(message *types.RunnerScaleSetMessage) error {
	p.logger.Infof("process message with id %d and type %s", message.MessageId, message.MessageType)

	if message.Statistics == nil {
		return fmt.Errorf("can't process message with empty statistics")
	}

	p.logger.Infof("Runner Set Statistics - Available: %d, Acquired: %d, Assigned: %d, Running: %d, Registered Runners: %d, Busy: %d, Idle: %d",
		message.Statistics.TotalAvailableJobs,
		message.Statistics.TotalAcquiredJobs,
		message.Statistics.TotalAssignedJobs,
		message.Statistics.TotalRunningJobs,
		message.Statistics.TotalRegisteredRunners,
		message.Statistics.TotalBusyRunners,
		message.Statistics.TotalIdleRunners,
	)

	if message.MessageType != runnerScaleSetJobMessagesType {
		p.logger.Infof("skip message with unknown message type %s", message.MessageType)
		return nil
	}

	var batchedMessages []json.RawMessage
	if message.Body != "" {
		if err := json.NewDecoder(strings.NewReader(message.Body)).Decode(&batchedMessages); err != nil {
			return fmt.Errorf("could not decode job messages. %w", err)
		}
	}

	p.logger.Infof("process batched runner scale set job messages with id %d and batch size %d", message.MessageId, len(batchedMessages))

	inflight := int(p.provisioningInflight.Load())
	requiredRunners := message.Statistics.TotalAssignedJobs - message.Statistics.TotalRegisteredRunners - inflight
	provisionedRunners := 0

	p.logger.Infof("provisioning inflight: %d, required runners: %d", inflight, requiredRunners)

	if message.MessageId == 0 && len(batchedMessages) == 0 {
		// The session-creation snapshot can lag: runners whose jobs are already
		// running may not be counted in TotalRegisteredRunners yet. A running job
		// implies a live runner picked it up (unlike TotalBusyRunners, which only
		// means a job is pinned to a runner record - possibly one that never
		// connected), so trust whichever count is larger.
		effectiveRegisteredRunners := max(message.Statistics.TotalRegisteredRunners, message.Statistics.TotalRunningJobs)
		requiredRunners = message.Statistics.TotalAssignedJobs - effectiveRegisteredRunners
		p.logger.Infof("initial message received, provision runners to cover assigned-job gap (effective registered runners: %d, required runners: %d)", effectiveRegisteredRunners, requiredRunners)
		for provisionedRunners < requiredRunners {
			provisionedRunners++
			p.logger.Infof("provisioning runner %d/%d to cover assigned-job gap", provisionedRunners, requiredRunners)
			go p.startRunner(newRecoveryJobIdentity())
		}
		return nil
	}

	var availableJobs []int64
	for _, message := range batchedMessages {
		var messageType types.JobMessageType
		if err := json.Unmarshal(message, &messageType); err != nil {
			return fmt.Errorf("could not decode job message type. %w", err)
		}

		switch messageType.MessageType {
		case "JobAvailable":
			var jobAvailable types.JobAvailable
			if err := json.Unmarshal(message, &jobAvailable); err != nil {
				return fmt.Errorf("could not decode job available message. %w", err)
			}
			p.logger.Infof("Job available message received for JobId: %s, RunnerRequestId: %d", jobAvailable.JobId, jobAvailable.RunnerRequestId)
			availableJobs = append(availableJobs, jobAvailable.RunnerRequestId)
		case "JobAssigned":
			var jobAssigned types.JobAssigned
			if err := json.Unmarshal(message, &jobAssigned); err != nil {
				return fmt.Errorf("could not decode job assigned message. %w", err)
			}

			p.logger.Infof("Job assigned message received for JobId: %s, RunnerRequestId: %d", jobAssigned.JobId, jobAssigned.RunnerRequestId)

			if provisionedRunners < requiredRunners {
				provisionedRunners++
				p.logger.Infof("number of runners provisioning started: %d. Max required runners: %d", provisionedRunners, requiredRunners)
				go p.startRunner(newJobIdentity(jobAssigned.JobId, jobAssigned.RunnerRequestId))
			}
		case "JobStarted":
			var jobStarted types.JobStarted
			if err := json.Unmarshal(message, &jobStarted); err != nil {
				return fmt.Errorf("could not decode job started message. %w", err)
			}
			p.logger.Infof("Job started message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d, RunnerName: %s", jobStarted.JobId, jobStarted.RunnerRequestId, jobStarted.RunnerId, jobStarted.RunnerName)
		case "JobCompleted":
			var jobCompleted types.JobCompleted
			if err := json.Unmarshal(message, &jobCompleted); err != nil {
				return fmt.Errorf("could not decode job completed message. %w", err)
			}

			p.logger.Infof("Job completed message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d, RunnerName: %s, with Result: %s", jobCompleted.JobId, jobCompleted.RunnerRequestId, jobCompleted.RunnerId, jobCompleted.RunnerName, jobCompleted.Result)

			runnerName := jobCompleted.RunnerName
			if runnerName != "" {
				p.cancelRunnerContext(runnerName, "Job completed webhook received")
			} else {
				p.logger.Warnf("Job completed message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d, but RunnerName is empty. Skipping cleanup.", jobCompleted.JobId, jobCompleted.RunnerRequestId, jobCompleted.RunnerId)
			}

			if (jobCompleted.JobId != "" || jobCompleted.RunnerRequestId != 0) && (jobCompleted.Result == cancelledStatus || jobCompleted.Result == ignoredStatus || jobCompleted.Result == abandonedStatus) {
				jobIdentity := jobIdentity{jobId: jobCompleted.JobId, runnerRequestId: jobCompleted.RunnerRequestId}
				p.logger.Infof("setting upstream canceled job for %s", jobIdentity)
				p.setUpstreamCanceledJob(jobIdentity)
			}
		default:
			p.logger.Infof("unknown job message type %s", messageType.MessageType)
		}
	}

	err := p.runnerManager.AcquireJobs(p.ctx, availableJobs)
	if err != nil {
		return fmt.Errorf("could not acquire jobs. %w", err)
	}

	return nil
}

func (p *RunnerMessageProcessor) AdoptVM(vmName string) {
	runnerContext, cancel := context.WithCancel(p.ctx)
	p.storeRunnerContextCancel(vmName, cancel)

	context.AfterFunc(runnerContext, func() {
		p.logger.Infof("cleaning up resources for %s after runner context is canceled", vmName)
		p.runnerProvisioner.CleanupResources(context.WithoutCancel(p.ctx), vmName)
		p.vmTracker.Untrack(vmName)
	})

	p.vmTracker.Track(vmName)
}

func (p *RunnerMessageProcessor) startRunner(job jobIdentity) {
	p.provisioningInflight.Add(1)
	var executionErr error

	defer p.removeUpstreamCanceledJob(job)

	executor, commands, provisioningErr := p.provisionRunnerWithRetry(p.ctx, job)
	p.provisioningInflight.Add(-1)
	if provisioningErr != nil {
		if errors.Is(provisioningErr, context.Canceled) {
			p.logger.Infof("provisioning canceled for %s", p.runnerScaleSetName)
		} else {
			p.logger.Errorf("unable to provision Orka runner for %s: %v", p.runnerScaleSetName, provisioningErr)
		}
		return
	}

	if executor == nil {
		p.logger.Errorf("provisioning returned nil executor for %s", p.runnerScaleSetName)
		return
	}

	runnerContext, cancel := context.WithCancel(p.ctx)
	p.storeRunnerContextCancel(executor.VMName, cancel)

	context.AfterFunc(runnerContext, func() {
		p.logger.Infof("cleaning up resources for %s after runner context is canceled", executor.VMName)
		p.runnerProvisioner.CleanupResources(context.WithoutCancel(p.ctx), executor.VMName)
		p.vmTracker.Untrack(executor.VMName)
	})

	defer func() {
		if isNetworkingFailure(executionErr) {
			p.logger.Warnf("SSH connection dropped for %s (%v). Skipping cleanup, relying on JobCompleted webhook.", job, executionErr)
			return
		}

		var cancelReason string
		var exitErr *ssh.ExitError

		if errors.Is(executionErr, context.Canceled) {
			cancelReason = "runner context was canceled"
			p.logger.Infof("runner context canceled for RunnerName %s with %s. Cleaning up resources.", executor.VMName, job)
		} else if executionErr != nil {
			if errors.As(executionErr, &exitErr) {
				cancelReason = fmt.Sprintf("execution failed with exit code %d", exitErr.ExitStatus())
				p.logger.Errorf("execution failed with exit code %d for RunnerName %s with %s. Cleaning up resources.", exitErr.ExitStatus(), executor.VMName, job)
			} else {
				cancelReason = fmt.Sprintf("execution failed: %v", executionErr)
				p.logger.Errorf("execution failed for RunnerName %s with %s. Cleaning up resources: %v", executor.VMName, job, executionErr)
			}
		} else {
			cancelReason = "execution completed successfully"
			p.logger.Infof("execution completed successfully for RunnerName %s with %s. Cleaning up resources.", executor.VMName, job)
		}

		p.cancelRunnerContext(executor.VMName, cancelReason)
	}()

	p.vmTracker.Track(executor.VMName)
	executionErr = p.executeJobCommands(runnerContext, job, executor, commands)
}

// provisionRunnerWithRetry provisions a runner for job, retrying with backoff up to
// maxProvisionAttempts times. The attempts and the waits between them share one context, which is
// cancelled if the job is canceled upstream, so neither a long emulator deploy nor a wait of up to
// two minutes outlives the job.
//
// Giving up does not strand the job. GitHub cancels an assignment that no runner picks up within
// about 5 minutes and reissues the job under a new job ID, which arrives as a fresh JobAssigned and
// starts a new round of attempts. Because the backoff alone waits nearly 4 minutes, that reissue
// usually cancels a round before its last attempt, so the cap mostly ends rounds whose attempts fail
// fast. The error log and the exhausted metric record the rounds that do run out.
func (p *RunnerMessageProcessor) provisionRunnerWithRetry(ctx context.Context, job jobIdentity) (*orka.VMCommandExecutor, []string, error) {
	jobCtx, ok := p.beginProvisioning(ctx, job)
	if !ok {
		return nil, nil, fmt.Errorf("job %s was canceled upstream: %w", job, context.Canceled)
	}
	defer p.endProvisioning(job)

	retryBackoff := p.newProvisionBackoff()
	var lastErr error

	for attempt := 1; attempt <= maxProvisionAttempts; attempt++ {
		executor, commands, err := p.runnerProvisioner.ProvisionRunner(jobCtx)

		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		if err == nil {
			return executor, commands, nil
		}

		if p.isUpstreamCanceled(job) {
			return nil, nil, fmt.Errorf("job %s was canceled upstream during provisioning: %w", job, context.Canceled)
		}

		lastErr = err
		p.logger.Errorf(
			"unable to provision Orka runner for %s and job %s (attempt %d/%d). More information: %v",
			p.runnerScaleSetName,
			job,
			attempt,
			maxProvisionAttempts,
			err,
		)

		if attempt == maxProvisionAttempts {
			break
		}

		wait := retryBackoff.NextBackOff()
		if wait == backoff.Stop {
			break
		}

		select {
		case <-jobCtx.Done():
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			return nil, nil, fmt.Errorf("job %s was canceled upstream while waiting to retry: %w", job, context.Canceled)
		case <-time.After(wait):
		}
	}

	// The last attempt's error was logged just above, and the caller logs the returned error, so it
	// is not repeated here.
	p.logger.Errorf(
		"giving up on provisioning a runner for %s and job %s after %d attempts. GitHub reissues the job if no runner picks it up within about 5 minutes, which starts a new round of attempts.",
		p.runnerScaleSetName,
		job,
		maxProvisionAttempts,
	)
	p.metrics.IncRunnerProvisionExhausted()

	return nil, nil, fmt.Errorf("unable to provision Orka runner for %s and job %s after %d attempts: %w", p.runnerScaleSetName, job, maxProvisionAttempts, lastErr)
}

// beginProvisioning derives the context for provisioning a job and registers its cancel func, unless
// the job has already been canceled upstream. The check and the registration share a lock with
// setUpstreamCanceledJob, so a cancellation can never slip in between them unnoticed.
func (p *RunnerMessageProcessor) beginProvisioning(ctx context.Context, job jobIdentity) (context.Context, bool) {
	p.upstreamCanceledJobsMutex.Lock()
	defer p.upstreamCanceledJobsMutex.Unlock()

	if p.upstreamCanceledJobs[job] {
		return nil, false
	}

	jobCtx, cancel := context.WithCancel(ctx)
	p.provisionCancels[job] = cancel

	return jobCtx, true
}

func (p *RunnerMessageProcessor) endProvisioning(job jobIdentity) {
	p.upstreamCanceledJobsMutex.Lock()
	defer p.upstreamCanceledJobsMutex.Unlock()

	if cancel, ok := p.provisionCancels[job]; ok {
		cancel()
		delete(p.provisionCancels, job)
	}
}

func (p *RunnerMessageProcessor) executeJobCommands(ctx context.Context, job jobIdentity, executor *orka.VMCommandExecutor, commands []string) error {
	p.logger.Infof("starting execution for %s on VM %s", job, executor.VMName)

	err := executor.ExecuteCommands(ctx, commands...)

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		p.logger.Errorf("execution failed for %s on VM %s: %v", job, executor.VMName, err)
		return err
	}

	p.logger.Infof("execution completed for %s on VM %s", job, executor.VMName)
	return nil
}

func (p *RunnerMessageProcessor) isUpstreamCanceled(job jobIdentity) bool {
	p.upstreamCanceledJobsMutex.RLock()
	defer p.upstreamCanceledJobsMutex.RUnlock()
	return p.upstreamCanceledJobs[job]
}

func (p *RunnerMessageProcessor) setUpstreamCanceledJob(job jobIdentity) {
	p.upstreamCanceledJobsMutex.Lock()
	defer p.upstreamCanceledJobsMutex.Unlock()
	p.upstreamCanceledJobs[job] = true

	// Stop provisioning that is still running, which may be minutes into an emulator deploy or
	// waiting to retry. The provisioner cleans up whatever it had brought up.
	if cancel, ok := p.provisionCancels[job]; ok {
		p.logger.Infof("interrupting in-flight provisioning for %s, which was canceled upstream", job)
		cancel()
	}
}

func (p *RunnerMessageProcessor) removeUpstreamCanceledJob(job jobIdentity) {
	p.upstreamCanceledJobsMutex.Lock()
	defer p.upstreamCanceledJobsMutex.Unlock()
	delete(p.upstreamCanceledJobs, job)
}

func (p *RunnerMessageProcessor) storeRunnerContextCancel(runnerName string, cancel context.CancelFunc) {
	p.runnerContextCancelsMutex.Lock()
	defer p.runnerContextCancelsMutex.Unlock()
	p.runnerContextCancels[runnerName] = cancel
}

func (p *RunnerMessageProcessor) cancelRunnerContext(runnerName string, reason string) {
	p.runnerContextCancelsMutex.Lock()
	defer p.runnerContextCancelsMutex.Unlock()

	if cancel, exists := p.runnerContextCancels[runnerName]; exists {
		p.logger.Infof("canceling runner context for RunnerName: %s. Triggered by: %s", runnerName, reason)
		cancel()
		delete(p.runnerContextCancels, runnerName)
	} else {
		p.logger.Debugf("runner context for RunnerName: %s already canceled or not found. Triggered by: %s", runnerName, reason)
	}
}

func isNetworkingFailure(err error) bool {
	if err == nil {
		return false
	}

	var exitErr *ssh.ExitError
	return !errors.As(err, &exitErr) && !errors.Is(err, context.Canceled)
}
