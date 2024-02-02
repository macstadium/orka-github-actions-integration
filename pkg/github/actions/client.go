package actions

import (
	"context"
	"fmt"
	"net/http"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

const scaleSetEndpoint = "_apis/runtime/runnerscalesets"

type ActionsClient struct {
	*retryablehttp.Client

	AuthorizationInfo *types.AuthorizationInfo
}

func (client *ActionsClient) GetRunnersList(ctx context.Context, runnerGroupId int, runnerName string) (*types.RunnersListResponse, error) {
	path := fmt.Sprintf("%s/%s?runnerGroupId=%d&name=%s", client.AuthorizationInfo.ActionsServiceUrl, scaleSetEndpoint, runnerGroupId, runnerName)

	return api.RequestJSON[any, types.RunnersListResponse](ctx, client.Client, http.MethodGet, path, nil)
}

func (client *ActionsClient) CreateRunner(ctx context.Context, runner *types.Runner) (*types.Runner, error) {
	return api.RequestJSON[types.Runner, types.Runner](ctx, client.Client, http.MethodPost, scaleSetEndpoint, runner)
}

func NewActionsClient(authInfo *types.AuthorizationInfo) (*ActionsClient, error) {
	retryableClient, err := retryablehttp.NewClient(&retryablehttp.ClientTransport{
		Token:       authInfo.AdminToken,
		ContentType: "application/json",
	})

	if err != nil {
		return nil, err
	}

	return &ActionsClient{
		AuthorizationInfo: authInfo,
		Client:            retryableClient,
	}, nil
}
