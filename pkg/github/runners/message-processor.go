package runners

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
)

type RunnerScaleSettings struct {
	RunnerName string
	MinRunners int
	MaxRunners int
}

func NewRunnerMessageProcessor(ctx context.Context, runnerManager RunnerManagerInterface, runnerProvisioner RunnerProvisionerInterface, settings *RunnerScaleSettings) *RunnerMessageProcessor {
	return &RunnerMessageProcessor{
		ctx:               ctx,
		runnerManager:     runnerManager,
		runnerProvisioner: runnerProvisioner,
		settings:          settings,
		logger:            logging.Logger,
	}
}

func (p *RunnerMessageProcessor) StartProcessingMessages() error {
	for {
		p.logger.Infof("waiting for message for runner %s...", p.settings.RunnerName)
		select {
		case <-p.ctx.Done():
			p.logger.Infof("message processing service is stopped for runner %s", p.settings.RunnerName)
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
			p.logger.Infof("Job available message received for RunnerRequestId: %d", jobAvailable.RunnerRequestId)
			availableJobs = append(availableJobs, jobAvailable.RunnerRequestId)
		case "JobAssigned":
			var jobAssigned types.JobAssigned
			if err := json.Unmarshal(message, &jobAssigned); err != nil {
				return fmt.Errorf("could not decode job assigned message. %w", err)
			}

			p.logger.Infof("Job assigned message received for RunnerRequestId: %d", jobAssigned.RunnerRequestId)

			go func() {
				err := p.runnerProvisioner.ProvisionJITRunner(p.ctx, fmt.Sprintf("%s-%d-%d", p.settings.RunnerName, jobAssigned.RunnerRequestId, jobAssigned.WorkflowRunId))
				if err != nil {
					p.logger.Errorf("unable to provision Orka runner for %s. More information: %s", p.settings.RunnerName, err.Error())
				}
			}()
		case "JobStarted":
			var jobStarted types.JobStarted
			if err := json.Unmarshal(message, &jobStarted); err != nil {
				return fmt.Errorf("could not decode job started message. %w", err)
			}
			p.logger.Infof("Job started message received for RunnerRequestId: %d and RunnerId: %d", jobStarted.RunnerRequestId, jobStarted.RunnerId)
		case "JobCompleted":
			var jobCompleted types.JobCompleted
			if err := json.Unmarshal(message, &jobCompleted); err != nil {
				return fmt.Errorf("could not decode job completed message. %w", err)
			}

			p.logger.Infof("Job completed message received for RunnerRequestId: %d, RunnerId: %d, RunnerName: %s, with Result: %s", jobCompleted.RunnerRequestId, jobCompleted.RunnerId, jobCompleted.RunnerName, jobCompleted.Result)
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
