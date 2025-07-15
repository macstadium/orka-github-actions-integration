// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package runners

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	ghErrors "github.com/macstadium/orka-github-actions-integration/pkg/github/errors"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/messagequeue"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
)

const (
	sessionCreationMaxRetryCount  = 10
	runnerScaleSetJobMessagesType = "RunnerScaleSetJobMessages"
)

func NewRunnerManager(ctx context.Context, client actions.ActionsService, runnerScaleSetId int) (*RunnerManager, error) {
	logger := logging.Logger.Named(fmt.Sprintf("runner-manager-%d", runnerScaleSetId))

	manager := RunnerManager{
		logger:           logger,
		runnerScaleSetId: runnerScaleSetId,
		actionsClient:    client,
	}

	session, err := createSessionWithRetry(ctx, logger, client, runnerScaleSetId)
	if err != nil {
		return nil, fmt.Errorf("failed to create session. %w", err)
	}

	initialMessage, err := manager.getInitialMessage(ctx, session)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial message: %w", err)
	}

	manager.lastMessageId = 0
	manager.initialMessage = initialMessage
	manager.messageQueueManager = messagequeue.NewMessageQueueManager(client, session)

	return &manager, nil
}

func createSessionWithRetry(ctx context.Context, logger *zap.SugaredLogger, client actions.ActionsService, runnerScaleSetId int) (*types.RunnerScaleSetSession, error) {
	sessionName, err := os.Hostname()
	if err != nil {
		sessionName = uuid.New().String()
		logger.Infof("could not get hostname, fail back to a random string %s", sessionName)
	}

	var runnerScaleSetSession *types.RunnerScaleSetSession
	var retryCount int
	for {
		runnerScaleSetSession, err = client.CreateMessageSession(ctx, runnerScaleSetId, sessionName)
		if err == nil {
			break
		}

		clientSideError := &ghErrors.HttpClientSideError{}
		if errors.As(err, &clientSideError) && clientSideError.Code != http.StatusConflict {
			logger.Info("unable to create message session. The error indicates something is wrong on the client side, won't make any retry.")
			return nil, fmt.Errorf("create message session http request failed. %w", err)
		}

		retryCount++
		if retryCount >= sessionCreationMaxRetryCount {
			return nil, fmt.Errorf("create message session failed since it exceed %d retry limit. %w", sessionCreationMaxRetryCount, err)
		}

		logger.Infof("unable to create message session. Will try again in 30 seconds. Detailed error: %s", err.Error())

		time.Sleep(time.Second * 30)
	}

	return runnerScaleSetSession, nil
}

func (m *RunnerManager) getInitialMessage(ctx context.Context, session *types.RunnerScaleSetSession) (*types.RunnerScaleSetMessage, error) {
	statistics, _ := json.Marshal(session.Statistics)
	m.logger.Infof("current runner scale set statistics %s", string(statistics))

	if session.Statistics.TotalAvailableJobs > 0 || session.Statistics.TotalAssignedJobs > 0 {
		acquirableJobs, err := m.actionsClient.GetAcquirableJobs(ctx, m.runnerScaleSetId)
		if err != nil {
			return nil, fmt.Errorf("get acquirable jobs failed. %w", err)
		}

		acquirableJobsJson, err := json.Marshal(acquirableJobs.Jobs)
		if err != nil {
			return nil, fmt.Errorf("marshal acquirable jobs failed. %w", err)
		}

		initialMessage := &types.RunnerScaleSetMessage{
			MessageId:   0,
			MessageType: runnerScaleSetJobMessagesType,
			Statistics:  session.Statistics,
			Body:        string(acquirableJobsJson),
		}

		return initialMessage, nil
	}

	return &types.RunnerScaleSetMessage{
		MessageId:   0,
		MessageType: runnerScaleSetJobMessagesType,
		Statistics:  session.Statistics,
		Body:        "",
	}, nil
}

func (m *RunnerManager) Close() error {
	m.logger.Infof("closing message queue for runner %d", m.runnerScaleSetId)
	return m.messageQueueManager.Close()
}

func (m *RunnerManager) ProcessMessages(ctx context.Context, handler func(msg *types.RunnerScaleSetMessage) error) error {
	if m.initialMessage != nil {
		err := handler(m.initialMessage)
		if err != nil {
			return fmt.Errorf("fail to process initial message. %w", err)
		}

		m.initialMessage = nil
		return nil
	}

	for {
		message, err := m.messageQueueManager.ReceiveNextMessage(ctx, m.lastMessageId)
		if err != nil {
			if ctx.Err() == context.Canceled {
				return err
			}
			m.logger.Errorf("unable to get the next message from the message queue. %w", err)
		}

		if message == nil {
			continue
		}

		err = handler(message)
		if err != nil {
			return fmt.Errorf("unable to handle message %v. %w", message, err)
		}

		m.lastMessageId = message.MessageId

		return m.deleteMessage(ctx, message.MessageId)
	}
}

func (m *RunnerManager) deleteMessage(ctx context.Context, messageID int64) error {
	err := m.messageQueueManager.DeleteMessage(ctx, messageID)
	if err != nil {
		return fmt.Errorf("unable to delete message with id %d. %w", messageID, err)
	}

	m.logger.Infof("successfully deleted message with ID %d", messageID)

	return nil
}

func (m *RunnerManager) AcquireJobs(ctx context.Context, requestIds []int64) error {
	m.logger.Infof("Acquiring jobs. Number of requests: %d, Request IDs: %s", len(requestIds), fmt.Sprint(requestIds))
	if len(requestIds) == 0 {
		return nil
	}

	ids, err := m.messageQueueManager.AcquireJobs(ctx, requestIds)
	if err != nil {
		return fmt.Errorf("unable to acquire jobs from the queue. %w", err)
	}

	m.logger.Infof("Successfully acquired jobs. Requested: %d, Acquired: %d", len(requestIds), len(ids))
	return nil
}
