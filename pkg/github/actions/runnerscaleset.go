// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"context"
	"fmt"
	"net/http"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
)

func (client *ActionsClient) GetRunnerScaleSet(ctx context.Context, runnerGroupId int, runnerName string) (*types.RunnerScaleSet, error) {
	path := fmt.Sprintf("/%s?runnerGroupId=%d&name=%s", scaleSetEndpoint, runnerGroupId, runnerName)

	runnerScaleSetList, err := RequestJSON[any, types.RunnersListResponse](ctx, client, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if runnerScaleSetList.Count == 0 {
		return nil, nil
	}

	if runnerScaleSetList.Count > 1 {
		return nil, fmt.Errorf("multiple runner scale sets found with name %s", runnerName)
	}

	return &runnerScaleSetList.Runners[0], nil
}

func (client *ActionsClient) CreateRunnerScaleSet(ctx context.Context, runner *types.RunnerScaleSet) (*types.RunnerScaleSet, error) {
	return RequestJSON[types.RunnerScaleSet, types.RunnerScaleSet](ctx, client, http.MethodPost, scaleSetEndpoint, runner)
}

func (client *ActionsClient) DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error {
	path := fmt.Sprintf("/%s/%d", scaleSetEndpoint, runnerScaleSetId)

	operation := func() error {
		_, err := RequestJSON[any, any](ctx, client, http.MethodDelete, path, nil)

		return err
	}

	return backoff.Retry(operation, backoff.NewExponentialBackOff())
}
