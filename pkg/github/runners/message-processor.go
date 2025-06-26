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
	cancelledStatus = "canceled"
	ignoredStatus   = "ignored"
	abandonedStatus = "abandoned"
	defaultJobId    = "missing-job-id"
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
				go func() {
					jobId := jobAssigned.JobId
					if jobId == "" {
						jobId = defaultJobId
					}

					for attempt := 1; !p.isCanceled(jobId); attempt++ {
						err := p.runnerProvisioner.ProvisionRunner(p.ctx)
						if err == nil {
							break
						}

						p.logger.Errorf("unable to provision Orka runner for %s (attempt %d). More information: %s", p.runnerScaleSetName, attempt, err.Error())

						time.Sleep(15 * time.Second)
					}

					p.removeCanceledJob(jobId)
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

			if jobCompleted.JobId != "" && (jobCompleted.Result == cancelledStatus || jobCompleted.Result == ignoredStatus || jobCompleted.Result == abandonedStatus) {
				p.setCanceledJob(jobCompleted.JobId)
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
