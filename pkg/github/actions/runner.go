// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"context"
	"fmt"
	"net/http"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
)

const (
	runnerEndpoint = "_apis/distributedtask/pools/0/agents"
)

func (client *ActionsClient) GetRunner(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
	path := fmt.Sprintf("/%s?agentName=%s", runnerEndpoint, runnerName)

	runnersList, err := RequestJSON[any, types.RunnerReferenceList](ctx, client, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if runnersList.Count == 0 {
		return nil, nil
	}

	if runnersList.Count > 1 {
		return nil, fmt.Errorf("multiple runner found with name %s", runnerName)
	}

	return &runnersList.RunnerReferences[0], nil
}

func (client *ActionsClient) CreateRunner(ctx context.Context, runnerScaleSetID int, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	path := fmt.Sprintf("/%s/%d/generatejitconfig", scaleSetEndpoint, runnerScaleSetID)

	jitRunnerSetting := &types.RunnerScaleSetJitRunnerSetting{
		Name: runnerName,
	}

	return RequestJSON[types.RunnerScaleSetJitRunnerSetting, types.RunnerScaleSetJitRunnerConfig](ctx, client, http.MethodPost, path, jitRunnerSetting)
}

func (client *ActionsClient) DeleteRunner(ctx context.Context, runnerID int) error {
	path := fmt.Sprintf("/%s/%d", runnerEndpoint, runnerID)

	_, err := RequestJSON[any, any](ctx, client, http.MethodDelete, path, nil)

	return err
}
