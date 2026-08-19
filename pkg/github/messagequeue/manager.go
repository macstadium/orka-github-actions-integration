// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package messagequeue

import (
	"context"
	"fmt"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
)

func NewMessageQueueManager(client actions.ActionsService, session *types.RunnerScaleSetSession) *MessageQueueManager {
	return &MessageQueueManager{
		client:  client,
		session: session,
		logger:  logging.Logger.Named(fmt.Sprintf("session-client-%d", session.RunnerScaleSet.Id)),
	}
}

func (m *MessageQueueManager) ReceiveNextMessage(ctx context.Context, lastMessageId int64) (*types.RunnerScaleSetMessage, error) {
	message, err := m.client.GetMessage(ctx, m.session.MessageQueueUrl, m.session.MessageQueueAccessToken, lastMessageId)
	if err != nil {
		return nil, fmt.Errorf("get message failed. %w", err)
	}

	return message, nil
}

func (m *MessageQueueManager) DeleteMessage(ctx context.Context, messageId int64) error {
	if err := m.client.DeleteMessage(ctx, m.session.MessageQueueUrl, m.session.MessageQueueAccessToken, messageId); err != nil {
		return fmt.Errorf("delete message failed. %w", err)
	}

	return nil
}

func (m *MessageQueueManager) AcquireJobs(ctx context.Context, requestIds []int64) ([]int64, error) {
	ids, err := m.client.AcquireJobs(ctx, m.session.RunnerScaleSet.Id, m.session.MessageQueueAccessToken, requestIds)
	if err != nil {
		return nil, fmt.Errorf("acquire jobs failed. %w", err)
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

	m.logger.Infof("deleting session with name %s and id %d.", m.session.OwnerName, m.session.SessionId.String())
	err := m.client.DeleteMessageSession(ctxWithTimeout, m.session.RunnerScaleSet.Id, m.session.SessionId)
	if err != nil {
		return fmt.Errorf("delete message session failed. %w", err)
	}

	m.session = nil

	return nil
}
