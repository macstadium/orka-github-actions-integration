package scalesetclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/actions/scaleset"
	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"go.uber.org/zap/exp/zapslog"
)

const (
	jobMessagesType     = "RunnerScaleSetJobMessages"
	messageAPIVersion   = "application/json; api-version=6.0-preview"
	unsupportedTypeText = "unsupported message type"
)

var statusCodePattern = regexp.MustCompile(`status="(\d{3})`)

type Client struct {
	sdk    *scaleset.Client
	logger *slog.Logger

	mu         sync.Mutex
	session    *scaleset.MessageSessionClient
	maxRunners int
}

func New(envData *env.Data, maxRunners int) (*Client, error) {
	logger := slog.New(zapslog.NewHandler(logging.Logger.Named("scaleset").Desugar().Core()))

	systemInfo := scaleset.SystemInfo{
		System:    "orka-github-actions-integration",
		Subsystem: "listener",
	}

	var sdk *scaleset.Client
	var err error

	if envData.GitHubPAT != "" {
		sdk, err = scaleset.NewClientWithPersonalAccessToken(scaleset.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     envData.GitHubURL,
			PersonalAccessToken: envData.GitHubPAT,
			SystemInfo:          systemInfo,
		}, scaleset.WithLogger(logger))
	} else {
		sdk, err = scaleset.NewClientWithGitHubApp(scaleset.ClientWithGitHubAppConfig{
			GitHubConfigURL: envData.GitHubURL,
			GitHubAppAuth: scaleset.GitHubAppAuth{
				ClientID:       fmt.Sprintf("%d", envData.GitHubAppID),
				InstallationID: envData.GitHubAppInstallationID,
				PrivateKey:     envData.GitHubAppPrivateKey,
			},
			SystemInfo: systemInfo,
		}, scaleset.WithLogger(logger))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create scaleset client: %w", err)
	}

	return &Client{sdk: sdk, logger: logger, maxRunners: maxRunners}, nil
}

func (c *Client) GetRunnerScaleSet(ctx context.Context, runnerGroupId int, runnerScaleSetName string) (*types.RunnerScaleSet, error) {
	got, err := c.sdk.GetRunnerScaleSet(ctx, runnerGroupId, runnerScaleSetName)
	if err != nil || got == nil {
		return nil, err
	}
	return toScaleSet(got), nil
}

func (c *Client) CreateRunnerScaleSet(ctx context.Context, runnerScaleSet *types.RunnerScaleSet) (*types.RunnerScaleSet, error) {
	created, err := c.sdk.CreateRunnerScaleSet(ctx, fromScaleSet(runnerScaleSet))
	if err != nil || created == nil {
		return nil, err
	}
	return toScaleSet(created), nil
}

func (c *Client) DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error {
	return c.sdk.DeleteRunnerScaleSet(ctx, runnerScaleSetId)
}

func (c *Client) GetRunner(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
	got, err := c.sdk.GetRunnerByName(ctx, runnerName)
	if err != nil || got == nil {
		return nil, err
	}
	return &types.RunnerReference{Id: got.ID, Name: got.Name, RunnerScaleSetId: got.RunnerScaleSetID}, nil
}

func (c *Client) CreateRunner(ctx context.Context, runnerScaleSetID int, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	cfg, err := c.sdk.GenerateJitRunnerConfig(ctx, &scaleset.RunnerScaleSetJitRunnerSetting{Name: runnerName}, runnerScaleSetID)
	if err != nil || cfg == nil {
		return nil, err
	}

	out := &types.RunnerScaleSetJitRunnerConfig{EncodedJITConfig: cfg.EncodedJITConfig}
	if cfg.Runner != nil {
		out.Runner = &types.RunnerReference{Id: cfg.Runner.ID, Name: cfg.Runner.Name, RunnerScaleSetId: cfg.Runner.RunnerScaleSetID}
	}
	return out, nil
}

func (c *Client) DeleteRunner(ctx context.Context, runnerID int) error {
	return c.sdk.RemoveRunner(ctx, int64(runnerID))
}

func (c *Client) CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*types.RunnerScaleSetSession, error) {
	sessionClient, err := c.sdk.MessageSessionClient(ctx, runnerScaleSetId, owner)
	if err != nil {
		return nil, withStatusCode(err)
	}

	c.mu.Lock()
	c.session = sessionClient
	c.mu.Unlock()

	return toSession(sessionClient.Session()), nil
}

func (c *Client) RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) (*types.RunnerScaleSetSession, error) {
	session, err := c.currentSession()
	if err != nil {
		return nil, err
	}
	return toSession(session.Session()), nil
}

func (c *Client) DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) error {
	session, err := c.currentSession()
	if err != nil {
		return err
	}
	return session.Close(ctx)
}

func (c *Client) AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error) {
	session, err := c.currentSession()
	if err != nil {
		return nil, err
	}
	return session.AcquireJobs(ctx, requestIds)
}

func (c *Client) GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64) (*types.RunnerScaleSetMessage, error) {
	session, err := c.currentSession()
	if err != nil {
		return nil, err
	}

	msg, err := session.GetMessage(ctx, int(lastMessageId), c.maxRunners)
	if err != nil {
		if strings.Contains(err.Error(), unsupportedTypeText) {
			return c.skippableMessage(ctx, session, lastMessageId)
		}
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}

	body, err := encodeBody(msg)
	if err != nil {
		return nil, err
	}

	return &types.RunnerScaleSetMessage{
		MessageId:   int64(msg.MessageID),
		MessageType: jobMessagesType,
		Body:        body,
		Statistics:  toStatistics(msg.Statistics),
	}, nil
}

func (c *Client) DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error {
	session, err := c.currentSession()
	if err != nil {
		return err
	}
	return session.DeleteMessage(ctx, int(messageId))
}

func (c *Client) GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*types.AcquirableJobList, error) {
	return &types.AcquirableJobList{Count: 0, Jobs: []types.AcquirableJob{}}, nil
}

func (c *Client) skippableMessage(ctx context.Context, session *scaleset.MessageSessionClient, lastMessageId int64) (*types.RunnerScaleSetMessage, error) {
	current := session.Session()

	target, err := url.Parse(current.MessageQueueURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse message queue url: %w", err)
	}
	if lastMessageId > 0 {
		query := target.Query()
		query.Set("lastMessageId", strconv.FormatInt(lastMessageId, 10))
		target.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", messageAPIVersion)
	req.Header.Set("Authorization", "Bearer "+current.MessageQueueAccessToken)
	req.Header.Set(scaleset.HeaderScaleSetMaxCapacity, strconv.Itoa(c.maxRunners))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to re-read unsupported message: status %s", resp.Status)
	}

	var envelope struct {
		MessageID   int64                          `json:"messageId"`
		MessageType string                         `json:"messageType"`
		Statistics  *types.RunnerScaleSetStatistic `json:"statistics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode unsupported message envelope: %w", err)
	}

	if envelope.MessageType == jobMessagesType {
		return nil, fmt.Errorf("message %d is decodable on retry, deferring to the SDK", envelope.MessageID)
	}

	c.logger.Warn("re-read unsupported message type so it can be acknowledged",
		slog.Int64("messageId", envelope.MessageID),
		slog.String("messageType", envelope.MessageType))

	return &types.RunnerScaleSetMessage{
		MessageId:   envelope.MessageID,
		MessageType: envelope.MessageType,
		Statistics:  envelope.Statistics,
	}, nil
}

func withStatusCode(err error) error {
	if err == nil {
		return nil
	}

	match := statusCodePattern.FindStringSubmatch(err.Error())
	if match == nil {
		return err
	}

	status, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return err
	}

	return &actions.ActionsError{StatusCode: status, Message: err.Error()}
}

func (c *Client) currentSession() (*scaleset.MessageSessionClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session == nil {
		return nil, fmt.Errorf("no message session has been created")
	}
	return c.session, nil
}

func encodeBody(msg *scaleset.RunnerScaleSetMessage) (string, error) {
	batch := make([]any, 0, len(msg.JobAvailableMessages)+len(msg.JobAssignedMessages)+len(msg.JobStartedMessages)+len(msg.JobCompletedMessages))
	for _, m := range msg.JobAvailableMessages {
		batch = append(batch, m)
	}
	for _, m := range msg.JobAssignedMessages {
		batch = append(batch, m)
	}
	for _, m := range msg.JobStartedMessages {
		batch = append(batch, m)
	}
	for _, m := range msg.JobCompletedMessages {
		batch = append(batch, m)
	}

	if len(batch) == 0 {
		return "", nil
	}

	encoded, err := json.Marshal(batch)
	if err != nil {
		return "", fmt.Errorf("failed to encode job messages: %w", err)
	}
	return string(encoded), nil
}

func toScaleSet(in *scaleset.RunnerScaleSet) *types.RunnerScaleSet {
	labels := make([]types.RunnerScaleSetLabel, 0, len(in.Labels))
	for _, l := range in.Labels {
		labels = append(labels, types.RunnerScaleSetLabel{Type: l.Type, Name: l.Name})
	}

	return &types.RunnerScaleSet{
		Id:                 in.ID,
		Name:               in.Name,
		RunnerGroupId:      in.RunnerGroupID,
		RunnerGroupName:    in.RunnerGroupName,
		Labels:             labels,
		RunnerSetting:      types.RunnerScaleSetSetting{DisableUpdate: in.RunnerSetting.DisableUpdate},
		CreatedOn:          in.CreatedOn,
		RunnerJitConfigUrl: in.RunnerJitConfigURL,
		Statistics:         toStatistics(in.Statistics),
	}
}

func fromScaleSet(in *types.RunnerScaleSet) *scaleset.RunnerScaleSet {
	labels := make([]scaleset.Label, 0, len(in.Labels))
	for _, l := range in.Labels {
		labels = append(labels, scaleset.Label{Type: l.Type, Name: l.Name})
	}

	return &scaleset.RunnerScaleSet{
		ID:              in.Id,
		Name:            in.Name,
		RunnerGroupID:   in.RunnerGroupId,
		RunnerGroupName: in.RunnerGroupName,
		Labels:          labels,
		RunnerSetting:   scaleset.RunnerSetting{DisableUpdate: in.RunnerSetting.DisableUpdate},
	}
}

func toSession(in scaleset.RunnerScaleSetSession) *types.RunnerScaleSetSession {
	sessionId := in.SessionID
	out := &types.RunnerScaleSetSession{
		SessionId:               &sessionId,
		OwnerName:               in.OwnerName,
		MessageQueueUrl:         in.MessageQueueURL,
		MessageQueueAccessToken: in.MessageQueueAccessToken,
		Statistics:              toStatistics(in.Statistics),
	}
	if in.RunnerScaleSet != nil {
		out.RunnerScaleSet = toScaleSet(in.RunnerScaleSet)
	}
	return out
}

func toStatistics(in *scaleset.RunnerScaleSetStatistic) *types.RunnerScaleSetStatistic {
	if in == nil {
		return nil
	}
	return &types.RunnerScaleSetStatistic{
		TotalAvailableJobs:     in.TotalAvailableJobs,
		TotalAcquiredJobs:      in.TotalAcquiredJobs,
		TotalAssignedJobs:      in.TotalAssignedJobs,
		TotalRunningJobs:       in.TotalRunningJobs,
		TotalRegisteredRunners: in.TotalRegisteredRunners,
		TotalBusyRunners:       in.TotalBusyRunners,
		TotalIdleRunners:       in.TotalIdleRunners,
	}
}
