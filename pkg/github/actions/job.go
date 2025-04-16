// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func (client *ActionsClient) GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*types.AcquirableJobList, error) {
	path := fmt.Sprintf("/%s/%d/acquirablejobs", scaleSetEndpoint, runnerScaleSetId)

	res, err := RequestJSON[any, types.AcquirableJobList](ctx, client, http.MethodGet, path, nil)
	if res == nil {
		res = &types.AcquirableJobList{Count: 0, Jobs: []types.AcquirableJob{}}
	}

	return res, err
}

func (client *ActionsClient) AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error) {
	path := fmt.Sprintf("%s%s/%d/acquirejobs?api-version=6.0-preview", client.actionsServiceUrl, scaleSetEndpoint, runnerScaleSetId)

	body, err := json.Marshal(requestIds)
	if err != nil {
		return nil, err
	}

	response, err := sendMessageQueueRequest(ctx, path, http.MethodPost, &retryablehttp.ClientTransport{
		Token:       messageQueueAccessToken,
		ContentType: "application/json",
	}, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, parseMessageQueueResponse(response)
	}

	var acquiredJobs *types.Int64List
	err = json.NewDecoder(response.Body).Decode(&acquiredJobs)
	if err != nil {
		return nil, err
	}

	return acquiredJobs.Value, nil
}
