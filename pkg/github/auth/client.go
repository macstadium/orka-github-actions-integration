// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/macstadium/orka-github-actions-integration/pkg/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func GetAuthorizationInfo(ctx context.Context, accessToken *types.AccessToken, githubApiUrl *string, config *github.GitHubConfig) (*types.AuthorizationInfo, error) {
	registrationToken, err := getRegistrationToken(ctx, githubApiUrl, config, accessToken.Token)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/actions/runner-registration", *githubApiUrl)

	body := &types.RegistrationPayload{
		Url:         config.URL,
		RunnerEvent: "register",
	}

	httpClient, err := retryablehttp.NewClient(&retryablehttp.ClientTransport{
		ContentType: "application/json",
		RemoteAuth:  registrationToken.Token,
	})
	if err != nil {
		return nil, err
	}

	return api.RequestJSON[types.RegistrationPayload, types.AuthorizationInfo](ctx, httpClient.Client, http.MethodPost, path, body)
}

func createRegistrationTokenPath(githubApiUrl *string, config *github.GitHubConfig) (string, error) {
	switch config.Scope {
	case github.GitHubScopeOrganization:
		path := fmt.Sprintf("%s/orgs/%s/actions/runners/registration-token", *githubApiUrl, config.Organization)
		return path, nil

	case github.GitHubScopeRepository:
		path := fmt.Sprintf("%s/repos/%s/%s/actions/runners/registration-token", *githubApiUrl, config.Organization, config.Repository)
		return path, nil

	default:
		return "", fmt.Errorf("unknown scope for config url: %s", config.URL)
	}
}

func getRegistrationToken(ctx context.Context, githubApiUrl *string, config *github.GitHubConfig, accessToken string) (*types.RegistrationToken, error) {
	path, err := createRegistrationTokenPath(githubApiUrl, config)
	if err != nil {
		return nil, err
	}

	httpClient, err := retryablehttp.NewClient(&retryablehttp.ClientTransport{
		Token:       accessToken,
		ContentType: "application/vnd.github.v3+json",
	})
	if err != nil {
		return nil, err
	}

	return api.RequestJSON[any, types.RegistrationToken](ctx, httpClient.Client, http.MethodPost, path, nil)
}
