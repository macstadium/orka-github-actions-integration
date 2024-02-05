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
)

func main() {
	envData := env.ParseEnv()

	ctx := context.TODO()

	config, err := github.NewGitHubConfig(envData.GitHubURL)
	if err != nil {
		panic(err)
	}

	accessToken, err := app.FetchAccessToken(ctx, envData)
	if err != nil {
		panic(err)
	}

	authInfo, err := auth.GetAuthorizationInfo(ctx, accessToken, config)
	if err != nil {
		panic(err)
	}

	actionsClient, err := actions.NewActionsClient(authInfo)
	if err != nil {
		panic(err)
	}

	runnerScaleSet, err := actionsClient.GetRunnerScaleSet(ctx, constants.DefaultRunnerGroupID, envData.Runners[0].Name)
	if err != nil {
		panic(err)
	}

	fmt.Println("runnerScaleSet", runnerScaleSet)
}
