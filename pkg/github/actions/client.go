package actions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/app"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/auth"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/utils"
	"go.uber.org/zap"
)

const (
	scaleSetEndpoint = "_apis/runtime/runnerscalesets"
	apiVersion       = "6.0-preview"
)

type ActionsService interface {
	GetRunnerScaleSet(ctx context.Context, runnerGroupId int, runnerScaleSetName string) (*types.RunnerScaleSet, error)
	CreateRunnerScaleSet(ctx context.Context, runnerScaleSet *types.RunnerScaleSet) (*types.RunnerScaleSet, error)
	DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error

	GenerateJitRunnerConfig(ctx context.Context, runnerScaleSetID int, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error)

	CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*types.RunnerScaleSetSession, error)
	DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) error
	RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) (*types.RunnerScaleSetSession, error)

	AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error)
	GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*types.AcquirableJobList, error)

	GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64) (*types.RunnerScaleSetMessage, error)
	DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error
}

type ActionsClient struct {
	*retryablehttp.Client

	actionsServiceUrl   string
	adminToken          string
	adminTokenExpiresAt time.Time

	logger *zap.SugaredLogger

	envData *env.Data

	gitHubConfig *github.GitHubConfig

	// lock for refreshing the adminToken and adminTokenExpiresAt
	mu sync.Mutex
}

func (client *ActionsClient) newActionsServiceRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	if err := client.updateTokenIfNeeded(ctx); err != nil {
		return nil, err
	}

	targetURL, err := client.buildURL(path, apiVersion)
	if err != nil {
		return nil, err
	}

	return http.NewRequestWithContext(ctx, method, targetURL.String(), body)
}

func (client *ActionsClient) updateTokenIfNeeded(ctx context.Context) error {
	client.mu.Lock()
	defer client.mu.Unlock()

	aboutToExpire := time.Now().Add(60 * time.Second).After(client.adminTokenExpiresAt)
	if !aboutToExpire && !client.adminTokenExpiresAt.IsZero() {
		return nil
	}

	client.logger.Infof("refreshing token for githubConfigUrl %s", client.gitHubConfig.URL)

	accessToken, err := app.FetchAccessToken(ctx, client.envData)
	if err != nil {
		return fmt.Errorf("failed to get app access token on refresh: %w", err)
	}

	authInfo, err := auth.GetAuthorizationInfo(ctx, accessToken, client.gitHubConfig)
	if err != nil {
		return fmt.Errorf("failed to get actions service admin authentication info on refresh: %w", err)
	}

	client.actionsServiceUrl = authInfo.ActionsServiceUrl
	client.adminToken = authInfo.AdminToken
	client.adminTokenExpiresAt, err = utils.GetTokenExpirationTime(authInfo.AdminToken)
	if err != nil {
		return fmt.Errorf("failed to get admin token expire at on refresh: %w", err)
	}

	client.Client.Transport = &retryablehttp.ClientTransport{
		ContentType: "application/json",
		Token:       client.adminToken,
	}

	return nil
}

func (client *ActionsClient) buildURL(path, apiVersion string) (*url.URL, error) {
	urlString := fmt.Sprintf("%s/%s", strings.TrimRight(client.actionsServiceUrl, "/"), strings.TrimLeft(path, "/"))

	targetURL, err := url.Parse(urlString)
	if err != nil {
		return nil, err
	}

	query := targetURL.Query()
	if query.Get("api-version") == "" {
		query.Set("api-version", apiVersion)
	}
	targetURL.RawQuery = query.Encode()

	return targetURL, nil
}

func NewActionsClient(ctx context.Context, envData *env.Data, config *github.GitHubConfig) (*ActionsClient, error) {
	accessToken, err := app.FetchAccessToken(ctx, envData)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token from app: %w", err)
	}

	authInfo, err := auth.GetAuthorizationInfo(ctx, accessToken, config)
	if err != nil {
		return nil, fmt.Errorf("failed to get actions service auth info: %w", err)
	}

	retryableClient, err := retryablehttp.NewClient(&retryablehttp.ClientTransport{
		Token:       authInfo.AdminToken,
		ContentType: "application/json",
	})
	if err != nil {
		return nil, err
	}

	return &ActionsClient{
		actionsServiceUrl: authInfo.ActionsServiceUrl,
		adminToken:        authInfo.AdminToken,
		Client:            retryableClient,
		logger:            logging.Logger.Named("actions-service"),
		envData:           envData,
		gitHubConfig:      config,
	}, nil
}
