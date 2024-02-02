package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func GetAuthorizationInfo(ctx context.Context, accessToken *types.AccessToken, config *github.GitHubConfig, httpClient *retryablehttp.Client) (*types.AuthorizationInfo, error) {
	registrationToken, err := getRegistrationToken(ctx, config, accessToken.Token, httpClient)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/actions/runner-registration", constants.BaseGitHubAPIPath)

	body := &types.RegistrationPayload{
		Url:         config.URL,
		RunnerEvent: "register",
	}

	httpClient.Client.Transport = &retryablehttp.ClientTransport{
		ContentType: "application/json",
		RemoteAuth:  registrationToken.Token,
	}

	return api.RequestJSON[types.RegistrationPayload, types.AuthorizationInfo](ctx, httpClient, http.MethodPost, path, body)
}

func createRegistrationTokenPath(config *github.GitHubConfig) (string, error) {
	switch config.Scope {
	case github.GitHubScopeOrganization:
		path := fmt.Sprintf("%s/orgs/%s/actions/runners/registration-token", constants.BaseGitHubAPIPath, config.Organization)
		return path, nil

	case github.GitHubScopeRepository:
		path := fmt.Sprintf("%s/repos/%s/%s/actions/runners/registration-token", constants.BaseGitHubAPIPath, config.Organization, config.Repository)
		return path, nil

	default:
		return "", fmt.Errorf("unknown scope for config url: %s", config.URL)
	}
}

func getRegistrationToken(ctx context.Context, config *github.GitHubConfig, accessToken string, httpClient *retryablehttp.Client) (*types.RegistrationToken, error) {
	path, err := createRegistrationTokenPath(config)
	if err != nil {
		return nil, err
	}

	httpClient.Client.Transport = &retryablehttp.ClientTransport{
		Token:       accessToken,
		ContentType: "application/vnd.github.v3+json",
	}

	return api.RequestJSON[any, types.RegistrationToken](ctx, httpClient, http.MethodPost, path, nil)
}
