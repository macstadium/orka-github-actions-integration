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

	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	"golang.org/x/crypto/ssh"
)

const (
	cancelledStatus = "canceled"
	ignoredStatus   = "ignored"
	abandonedStatus = "abandoned"
	defaultJobId    = "missing-job-id"
)

func NewRunnerMessageProcessor(ctx context.Context, runnerManager RunnerManagerInterface, runnerProvisioner RunnerProvisionerInterface, vmTracker *VMTracker, runnerScaleSet *types.RunnerScaleSet) *RunnerMessageProcessor {
	return &RunnerMessageProcessor{
		ctx:                       ctx,
		runnerManager:             runnerManager,
		runnerProvisioner:         runnerProvisioner,
		logger:                    logging.Logger.Named(fmt.Sprintf("runner-message-processor-%d", runnerScaleSet.Id)),
		runnerScaleSetName:        runnerScaleSet.Name,
		upstreamCanceledJobs:      map[string]bool{},
		upstreamCanceledJobsMutex: sync.RWMutex{},
		jobContextCancels:         map[string]context.CancelFunc{},
		jobContextCancelsMutex:    sync.Mutex{},
		vmTracker:                 vmTracker,
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

	if message.MessageId == 0 && message.Body == "" { // initial message with statistics only
		return nil
	}

	var batchedMessages []json.RawMessage
	if err := json.NewDecoder(strings.NewReader(message.Body)).Decode(&batchedMessages); err != nil {
		return fmt.Errorf("could not decode job messages. %w", err)
	}

	p.logger.Infof("process batched runner scale set job messages with id %d and batch size %d", message.MessageId, len(batchedMessages))

	requiredRunners := message.Statistics.TotalAssignedJobs - message.Statistics.TotalRegisteredRunners
	provisionedRunners := 0

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

				jobId := jobAssigned.JobId
				if jobId == "" {
					jobId = defaultJobId
				}

				jobContext, cancel := context.WithCancel(p.ctx)
				p.storeJobContextCancel(jobId, cancel)

				go func() {
					var executionErr error

					defer p.removeUpstreamCanceledJob(jobId)

					executor, commands, provisioningErr := p.provisionRunnerWithRetry(jobContext, jobId)
					if provisioningErr != nil {
						if errors.Is(provisioningErr, context.Canceled) {
							p.logger.Infof("provisioning canceled for %s", p.runnerScaleSetName)
						} else {
							p.logger.Errorf("unable to provision Orka runner for %s: %v", p.runnerScaleSetName, provisioningErr)
						}
						p.cancelJobContext(jobId, "provisioning failed")
						return
					}

					if executor == nil {
						p.logger.Errorf("provisioning returned nil executor for %s", p.runnerScaleSetName)
						p.cancelJobContext(jobId, "provisioning failed")
						return
					}

					context.AfterFunc(jobContext, func() {
						p.logger.Infof("cleaning up resources for %s after job context is canceled", executor.VMName)
						p.runnerProvisioner.CleanupResources(context.WithoutCancel(p.ctx), executor.VMName)
						p.vmTracker.Untrack(executor.VMName)
					})

					defer func() {
						if isNetworkingFailure(executionErr) {
							p.logger.Warnf("SSH connection dropped for JobId %s (%v). Skipping cleanup, relying on JobCompleted webhook.", jobId, executionErr)
							return
						}

						var cancelReason string
						var exitErr *ssh.ExitError

						if errors.Is(executionErr, context.Canceled) {
							cancelReason = "job context was canceled"
							p.logger.Infof("job context canceled for JobId %s. Cleaning up resources.", jobId)
						} else if executionErr != nil {
							if errors.As(executionErr, &exitErr) {
								cancelReason = fmt.Sprintf("execution failed with exit code %d", exitErr.ExitStatus())
								p.logger.Errorf("execution failed with exit code %d for JobId %s. Cleaning up resources.", exitErr.ExitStatus(), jobId)
							} else {
								cancelReason = fmt.Sprintf("execution failed: %v", executionErr)
								p.logger.Errorf("execution failed for JobId %s. Cleaning up resources: %v", jobId, executionErr)
							}
						} else {
							cancelReason = "execution completed successfully"
							p.logger.Infof("execution completed successfully for JobId %s. Cleaning up resources.", jobId)
						}

						p.cancelJobContext(jobId, cancelReason)
					}()

					p.vmTracker.Track(executor.VMName)
					executionErr = p.executeJobCommands(jobContext, jobId, executor, commands)
				}()
			}
		case "JobStarted":
			var jobStarted types.JobStarted
			if err := json.Unmarshal(message, &jobStarted); err != nil {
				return fmt.Errorf("could not decode job started message. %w", err)
			}
			p.logger.Infof("Job started message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d", jobStarted.JobId, jobStarted.RunnerRequestId, jobStarted.RunnerId)
		case "JobCompleted":
			var jobCompleted types.JobCompleted
			if err := json.Unmarshal(message, &jobCompleted); err != nil {
				return fmt.Errorf("could not decode job completed message. %w", err)
			}

			p.logger.Infof("Job completed message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d, RunnerName: %s, with Result: %s", jobCompleted.JobId, jobCompleted.RunnerRequestId, jobCompleted.RunnerId, jobCompleted.RunnerName, jobCompleted.Result)

			p.cancelJobContext(jobCompleted.JobId, "Job completed webhook received")

			if jobCompleted.JobId != "" && (jobCompleted.Result == cancelledStatus || jobCompleted.Result == ignoredStatus || jobCompleted.Result == abandonedStatus) {
				p.setUpstreamCanceledJob(jobCompleted.JobId)
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

func (p *RunnerMessageProcessor) provisionRunnerWithRetry(ctx context.Context, jobId string) (*orka.VMCommandExecutor, []string, error) {
	for attempt := 1; !p.isUpstreamCanceled(jobId); attempt++ {
		executor, commands, err := p.runnerProvisioner.ProvisionRunner(ctx)
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		if err == nil {
			return executor, commands, nil
		}

		p.logger.Errorf(
			"unable to provision Orka runner for %s (attempt %d). More information: %v",
			p.runnerScaleSetName,
			attempt,
			err,
		)

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}

	return nil, nil, fmt.Errorf("unable to provision Orka runner for %s", p.runnerScaleSetName)
}

func (p *RunnerMessageProcessor) executeJobCommands(ctx context.Context, jobId string, executor *orka.VMCommandExecutor, commands []string) error {
	p.logger.Infof("starting execution for JobId: %s on VM %s", jobId, executor.VMName)

	err := executor.ExecuteCommands(ctx, commands...)

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		p.logger.Errorf("execution failed for JobId: %s on VM %s: %v", jobId, executor.VMName, err)
		return err
	}

	p.logger.Infof("execution completed for JobId: %s on VM %s", jobId, executor.VMName)
	return nil
}

func (p *RunnerMessageProcessor) isUpstreamCanceled(jobId string) bool {
	p.upstreamCanceledJobsMutex.RLock()
	defer p.upstreamCanceledJobsMutex.RUnlock()
	return p.upstreamCanceledJobs[jobId]
}

func (p *RunnerMessageProcessor) setUpstreamCanceledJob(jobId string) {
	p.upstreamCanceledJobsMutex.Lock()
	defer p.upstreamCanceledJobsMutex.Unlock()
	p.upstreamCanceledJobs[jobId] = true
}

func (p *RunnerMessageProcessor) removeUpstreamCanceledJob(jobId string) {
	p.upstreamCanceledJobsMutex.Lock()
	defer p.upstreamCanceledJobsMutex.Unlock()
	delete(p.upstreamCanceledJobs, jobId)
}

func (p *RunnerMessageProcessor) storeJobContextCancel(jobId string, cancel context.CancelFunc) {
	p.jobContextCancelsMutex.Lock()
	defer p.jobContextCancelsMutex.Unlock()
	p.jobContextCancels[jobId] = cancel
}

func (p *RunnerMessageProcessor) cancelJobContext(jobId string, reason string) {
	p.jobContextCancelsMutex.Lock()
	defer p.jobContextCancelsMutex.Unlock()

	if cancel, exists := p.jobContextCancels[jobId]; exists {
		p.logger.Infof("canceling job context for JobId: %s. Triggered by: %s", jobId, reason)
		cancel()
		delete(p.jobContextCancels, jobId)
	} else {
		p.logger.Debugf("job context for JobId: %s already canceled or not found. Triggered by: %s", jobId, reason)
	}
}

func isNetworkingFailure(err error) bool {
	if err == nil {
		return false
	}

	var exitErr *ssh.ExitError
	return !errors.As(err, &exitErr) && !errors.Is(err, context.Canceled)
}
