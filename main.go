package main

import (
	"context"
	"fmt"

	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/app"
	auth "github.com/macstadium/orka-github-actions-integration/pkg/github/runners-auth"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func main() {
	envData := env.ParseEnv()

	context := context.TODO()

	config, err := github.NewGitHubConfig(envData.GitHubURL)
	if err != nil {
		panic(err)
	}

	httpClient, err := retryablehttp.NewClient(&retryablehttp.ClientTransport{})
	if err != nil {
		panic(err)
	}

	accessToken, err := app.FetchAccessToken(context, envData, httpClient)
	if err != nil {
		panic(err)
	}

	authInfo, err := auth.GetAuthorizationInfo(context, accessToken, config, httpClient)
	if err != nil {
		panic(err)
	}

	actionsClient, err := actions.NewActionsClient(authInfo)
	if err != nil {
		panic(err)
	}

	runnersList, err := actionsClient.GetRunnersList(context, constants.DefaultRunnerGroupID, envData.Runners[0].Name)
	if err != nil {
		panic(err)
	}

	fmt.Println("runnersList", runnersList)
}
