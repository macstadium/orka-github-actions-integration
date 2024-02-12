package messagequeue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	ghErrors "github.com/macstadium/orka-github-actions-integration/pkg/github/errors"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
)

func NewMessageQueueManager(client actions.ActionsService, session *types.RunnerScaleSetSession) *MessageQueueManager {
	return &MessageQueueManager{
		client:  client,
		session: session,
		logger:  logging.Logger.Named("session-client"),
	}
}

func (m *MessageQueueManager) ReceiveNextMessage(ctx context.Context, lastMessageId int64) (*types.RunnerScaleSetMessage, error) {
	message, err := m.client.GetMessage(ctx, m.session.MessageQueueUrl, m.session.MessageQueueAccessToken, lastMessageId)
	if err == nil {
		return message, nil
	}

	expiredError := &ghErrors.MessageQueueTokenExpiredError{}
	if !errors.As(err, &expiredError) {
		return nil, fmt.Errorf("get message failed. %w", err)
	}

	m.logger.Info("message queue token is expired during GetNextMessage, refreshing...")
	session, err := m.client.RefreshMessageSession(ctx, m.session.RunnerScaleSet.Id, m.session.SessionId)
	if err != nil {
		return nil, fmt.Errorf("unable to refresh message session. %w", err)
	}

	m.session = session
	message, err = m.client.GetMessage(ctx, m.session.MessageQueueUrl, m.session.MessageQueueAccessToken, lastMessageId)
	if err != nil {
		return nil, fmt.Errorf("delete message failed after refresh message session. %w", err)
	}

	return message, nil
}

func (m *MessageQueueManager) DeleteMessage(ctx context.Context, messageId int64) error {
	err := m.client.DeleteMessage(ctx, m.session.MessageQueueUrl, m.session.MessageQueueAccessToken, messageId)
	if err == nil {
		return nil
	}

	expiredError := &ghErrors.MessageQueueTokenExpiredError{}
	if !errors.As(err, &expiredError) {
		return fmt.Errorf("delete message failed. %w", err)
	}

	m.logger.Info("message queue token is expired during DeleteMessage, refreshing...")
	session, err := m.client.RefreshMessageSession(ctx, m.session.RunnerScaleSet.Id, m.session.SessionId)
	if err != nil {
		return fmt.Errorf("unable to refresh message session. %w", err)
	}

	m.session = session

	err = m.client.DeleteMessage(ctx, m.session.MessageQueueUrl, m.session.MessageQueueAccessToken, messageId)
	if err != nil {
		return fmt.Errorf("delete message failed after refresh message session. %w", err)
	}

	return nil

}

func (m *MessageQueueManager) AcquireJobs(ctx context.Context, requestIds []int64) ([]int64, error) {
	ids, err := m.client.AcquireJobs(ctx, m.session.RunnerScaleSet.Id, m.session.MessageQueueAccessToken, requestIds)
	if err == nil {
		return ids, nil
	}

	expiredError := &ghErrors.MessageQueueTokenExpiredError{}
	if !errors.As(err, &expiredError) {
		return nil, fmt.Errorf("acquire jobs failed. %w", err)
	}

	m.logger.Info("message queue token is expired during AcquireJobs, refreshing...")
	session, err := m.client.RefreshMessageSession(ctx, m.session.RunnerScaleSet.Id, m.session.SessionId)
	if err != nil {
		return nil, fmt.Errorf("unable to refresh message session. %w", err)
	}

	m.session = session

	ids, err = m.client.AcquireJobs(ctx, m.session.RunnerScaleSet.Id, m.session.MessageQueueAccessToken, requestIds)
	if err != nil {
		return nil, fmt.Errorf("acquire jobs failed after refresh message session. %w", err)
	}

	return ids, nil
}

func (m *MessageQueueManager) Close() error {
	if m.session == nil {
		m.logger.Info("session is already deleted")
		return nil
	}

	ctxWithTimeout, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	m.logger.Infof("deleting session with name %s and id %d.", m.session.OwnerName, *m.session.SessionId)
	err := m.client.DeleteMessageSession(ctxWithTimeout, m.session.RunnerScaleSet.Id, m.session.SessionId)
	if err != nil {
		return fmt.Errorf("delete message session failed. %w", err)
	}

	m.session = nil

	return nil
}
