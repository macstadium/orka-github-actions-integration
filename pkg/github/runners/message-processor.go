// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package runners

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
)

const (
	cancelledStatus           = "canceled"
	ignoredStatus             = "ignored"
	abandonedStatus           = "abandoned"
	defaultJobId              = "missing-job-id"
	maxProvisioningRetries    = 3
	provisioningRetryInterval = 15 * time.Second
	acquiredJobTimeout        = 5 * time.Minute
	provisioningTimeout       = 10 * time.Minute
)

func NewRunnerMessageProcessor(ctx context.Context, runnerManager RunnerManagerInterface, runnerProvisioner RunnerProvisionerInterface, runnerScaleSet *types.RunnerScaleSet) *RunnerMessageProcessor {
	return &RunnerMessageProcessor{
		ctx:                ctx,
		runnerManager:      runnerManager,
		runnerProvisioner:  runnerProvisioner,
		logger:             logging.Logger.Named(fmt.Sprintf("runner-message-processor-%d", runnerScaleSet.Id)),
		runnerScaleSetName: runnerScaleSet.Name,
		canceledJobs:       map[string]bool{},
		canceledJobsMutex:  sync.RWMutex{},
		acquiredJobs:       map[int64]*AcquiredJobInfo{},
		acquiredJobsMutex:  sync.RWMutex{},
	}
}

func (p *RunnerMessageProcessor) StartProcessingMessages() error {
	go p.monitorStuckJobs()

	for {
		p.logger.Infof("waiting for message for runner %s...", p.runnerScaleSetName)
		select {
		case <-p.ctx.Done():
			p.logger.Infof("message processing service is stopped for runner %s", p.runnerScaleSetName)
			p.logStuckJobs()
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

			if !p.isJobAcquired(jobAvailable.RunnerRequestId) {
				availableJobs = append(availableJobs, jobAvailable.RunnerRequestId)
			} else {
				p.logger.Warnf("Job %d already acquired, skipping", jobAvailable.RunnerRequestId)
			}
		case "JobAssigned":
			var jobAssigned types.JobAssigned
			if err := json.Unmarshal(message, &jobAssigned); err != nil {
				return fmt.Errorf("could not decode job assigned message. %w", err)
			}

			p.logger.Infof("Job assigned message received for JobId: %s, RunnerRequestId: %d", jobAssigned.JobId, jobAssigned.RunnerRequestId)

			p.updateAcquiredJobWithId(jobAssigned.RunnerRequestId, jobAssigned.JobId)

			if provisionedRunners < requiredRunners {
				provisionedRunners++
				p.logger.Infof("number of runners provisioning started: %d. Max required runners: %d", provisionedRunners, requiredRunners)
				go func(runnerRequestId int64, jobId string) {
					if jobId == "" {
						jobId = defaultJobId
					}

					provisioned := false
					for attempt := 1; attempt <= maxProvisioningRetries && !p.isCanceled(jobId); attempt++ {
						p.logger.Infof("Provisioning runner for job %s (RunnerRequestId: %d), attempt %d/%d", jobId, runnerRequestId, attempt, maxProvisioningRetries)

						// Create timeout context for this provisioning attempt
						provisionCtx, cancel := context.WithTimeout(p.ctx, provisioningTimeout)
						err := p.runnerProvisioner.ProvisionRunner(provisionCtx)
						cancel() // Clean up context resources

						if err == nil {
							p.logger.Infof("Successfully provisioned runner for job %s (RunnerRequestId: %d)", jobId, runnerRequestId)
							provisioned = true
							break
						}

						if err == context.DeadlineExceeded {
							p.logger.Errorf("Provisioning timeout for job %s (RunnerRequestId: %d) attempt %d/%d after %s", jobId, runnerRequestId, attempt, maxProvisioningRetries, provisioningTimeout)
						} else {
							p.logger.Errorf("Failed to provision runner for job %s (RunnerRequestId: %d) attempt %d/%d: %s", jobId, runnerRequestId, attempt, maxProvisioningRetries, err.Error())
						}

						if attempt < maxProvisioningRetries {
							time.Sleep(provisioningRetryInterval)
						}
					}

					if !provisioned && !p.isCanceled(jobId) {
						p.logger.Errorf("Exhausted all %d provisioning attempts for job %s (RunnerRequestId: %d). Job may be stuck in queue.", maxProvisioningRetries, jobId, runnerRequestId)
					}

					p.removeCanceledJob(jobId)
					p.removeAcquiredJob(runnerRequestId)
				}(jobAssigned.RunnerRequestId, jobAssigned.JobId)
			}
		case "JobStarted":
			var jobStarted types.JobStarted
			if err := json.Unmarshal(message, &jobStarted); err != nil {
				return fmt.Errorf("could not decode job started message. %w", err)
			}
			p.logger.Infof("Job started message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d", jobStarted.JobId, jobStarted.RunnerRequestId, jobStarted.RunnerId)
			p.removeAcquiredJob(jobStarted.RunnerRequestId)
		case "JobCompleted":
			var jobCompleted types.JobCompleted
			if err := json.Unmarshal(message, &jobCompleted); err != nil {
				return fmt.Errorf("could not decode job completed message. %w", err)
			}

			p.logger.Infof("Job completed message received for JobId: %s, RunnerRequestId: %d, RunnerId: %d, RunnerName: %s, with Result: %s", jobCompleted.JobId, jobCompleted.RunnerRequestId, jobCompleted.RunnerId, jobCompleted.RunnerName, jobCompleted.Result)

			p.removeAcquiredJob(jobCompleted.RunnerRequestId)

			if jobCompleted.JobId != "" && (jobCompleted.Result == cancelledStatus || jobCompleted.Result == ignoredStatus || jobCompleted.Result == abandonedStatus) {
				p.setCanceledJob(jobCompleted.JobId)
			}
		default:
			p.logger.Infof("unknown job message type %s", messageType.MessageType)
		}
	}

	if len(availableJobs) > 0 {
		err := p.runnerManager.AcquireJobs(p.ctx, availableJobs)
		if err != nil {
			return fmt.Errorf("could not acquire jobs. %w", err)
		}

		for _, requestId := range availableJobs {
			p.trackAcquiredJob(requestId, "")
		}
	}

	return nil
}

func (p *RunnerMessageProcessor) isCanceled(jobId string) bool {
	p.canceledJobsMutex.RLock()
	defer p.canceledJobsMutex.RUnlock()
	return p.canceledJobs[jobId]
}

func (p *RunnerMessageProcessor) setCanceledJob(jobId string) {
	p.canceledJobsMutex.Lock()
	defer p.canceledJobsMutex.Unlock()
	p.canceledJobs[jobId] = true
}

func (p *RunnerMessageProcessor) removeCanceledJob(jobId string) {
	p.canceledJobsMutex.Lock()
	defer p.canceledJobsMutex.Unlock()
	delete(p.canceledJobs, jobId)
}

func (p *RunnerMessageProcessor) trackAcquiredJob(runnerRequestId int64, jobId string) {
	p.acquiredJobsMutex.Lock()
	defer p.acquiredJobsMutex.Unlock()
	p.acquiredJobs[runnerRequestId] = &AcquiredJobInfo{
		RunnerRequestId: runnerRequestId,
		JobId:           jobId,
		AcquiredAt:      time.Now(),
		RetryCount:      0,
	}
	p.logger.Infof("Tracked acquired job: RunnerRequestId=%d, JobId=%s", runnerRequestId, jobId)
}

func (p *RunnerMessageProcessor) updateAcquiredJobWithId(runnerRequestId int64, jobId string) {
	p.acquiredJobsMutex.Lock()
	defer p.acquiredJobsMutex.Unlock()
	if job, exists := p.acquiredJobs[runnerRequestId]; exists {
		job.JobId = jobId
		p.logger.Infof("Updated acquired job with JobId: RunnerRequestId=%d, JobId=%s", runnerRequestId, jobId)
	} else {
		p.acquiredJobs[runnerRequestId] = &AcquiredJobInfo{
			RunnerRequestId: runnerRequestId,
			JobId:           jobId,
			AcquiredAt:      time.Now(),
			RetryCount:      0,
		}
		p.logger.Infof("Tracked acquired job: RunnerRequestId=%d, JobId=%s", runnerRequestId, jobId)
	}
}

func (p *RunnerMessageProcessor) removeAcquiredJob(runnerRequestId int64) {
	p.acquiredJobsMutex.Lock()
	defer p.acquiredJobsMutex.Unlock()
	if _, exists := p.acquiredJobs[runnerRequestId]; exists {
		p.logger.Infof("Removing tracked job: RunnerRequestId=%d", runnerRequestId)
		delete(p.acquiredJobs, runnerRequestId)
	}
}

func (p *RunnerMessageProcessor) isJobAcquired(runnerRequestId int64) bool {
	p.acquiredJobsMutex.RLock()
	defer p.acquiredJobsMutex.RUnlock()
	_, exists := p.acquiredJobs[runnerRequestId]
	return exists
}

func (p *RunnerMessageProcessor) getAcquiredJobs() []*AcquiredJobInfo {
	p.acquiredJobsMutex.RLock()
	defer p.acquiredJobsMutex.RUnlock()

	jobs := make([]*AcquiredJobInfo, 0, len(p.acquiredJobs))
	for _, job := range p.acquiredJobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (p *RunnerMessageProcessor) monitorStuckJobs() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.logStuckJobs()
		}
	}
}

func (p *RunnerMessageProcessor) logStuckJobs() {
	jobs := p.getAcquiredJobs()
	if len(jobs) == 0 {
		return
	}

	now := time.Now()
	for _, job := range jobs {
		elapsed := now.Sub(job.AcquiredAt)
		if elapsed > acquiredJobTimeout {
			p.logger.Warnf("Job stuck and will be cleaned up: RunnerRequestId=%d, JobId=%s, AcquiredAt=%s, Elapsed=%s",
				job.RunnerRequestId, job.JobId, job.AcquiredAt.Format(time.RFC3339), elapsed.String())

			// Mark job as canceled to stop any ongoing provisioning attempts
			if job.JobId != "" && job.JobId != defaultJobId {
				p.setCanceledJob(job.JobId)
				p.logger.Infof("Marked stuck job as canceled: JobId=%s", job.JobId)
			}

			// Remove from tracking to allow cleanup
			p.removeAcquiredJob(job.RunnerRequestId)
			p.logger.Infof("Removed stuck job from tracking: RunnerRequestId=%d", job.RunnerRequestId)
		}
	}
}
