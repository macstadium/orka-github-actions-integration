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

// ListRunnerScaleSets returns every scale set in the given runner group. It hits the
// same endpoint as GetRunnerScaleSet but omits the "name" filter - the underlying API
// already returns a list (RunnersListResponse), GetRunnerScaleSet just narrows it to
// one name and errors on more than one match. Added for the Track A fleet spike
// (OK-5518): the reconciler needs to diff "our scale sets" against desired state, which
// requires listing, not single-name lookup.
func (client *ActionsClient) ListRunnerScaleSets(ctx context.Context, runnerGroupId int) ([]types.RunnerScaleSet, error) {
	path := fmt.Sprintf("/%s?runnerGroupId=%d", scaleSetEndpoint, runnerGroupId)

	runnerScaleSetList, err := RequestJSON[any, types.RunnersListResponse](ctx, client, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	return runnerScaleSetList.Runners, nil
}

func (client *ActionsClient) CreateRunnerScaleSet(ctx context.Context, runner *types.RunnerScaleSet) (*types.RunnerScaleSet, error) {
	return RequestJSON[types.RunnerScaleSet, types.RunnerScaleSet](ctx, client, http.MethodPost, scaleSetEndpoint, runner)
}

func (client *ActionsClient) DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error {
	path := fmt.Sprintf("/%s/%d", scaleSetEndpoint, runnerScaleSetId)

	_, err := RequestJSON[any, any](ctx, client, http.MethodDelete, path, nil)

	return err
}
