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

func (client *ActionsClient) CreateRunnerScaleSet(ctx context.Context, runner *types.RunnerScaleSet) (*types.RunnerScaleSet, error) {
	return RequestJSON[types.RunnerScaleSet, types.RunnerScaleSet](ctx, client, http.MethodPost, scaleSetEndpoint, runner)
}

func (client *ActionsClient) DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error {
	path := fmt.Sprintf("/%s/%d", scaleSetEndpoint, runnerScaleSetId)

	_, err := RequestJSON[any, any](ctx, client, http.MethodDelete, path, nil)

	return err
}

func (client *ActionsClient) GenerateJitRunnerConfig(ctx context.Context, runnerScaleSetID int, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	path := fmt.Sprintf("/%s/%d/generatejitconfig", scaleSetEndpoint, runnerScaleSetID)

	jitRunnerSetting := &types.RunnerScaleSetJitRunnerSetting{
		Name: runnerName,
	}

	return RequestJSON[types.RunnerScaleSetJitRunnerSetting, types.RunnerScaleSetJitRunnerConfig](ctx, client, http.MethodPost, path, jitRunnerSetting)
}
