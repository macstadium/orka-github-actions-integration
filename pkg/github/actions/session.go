package actions

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
)

func (client *ActionsClient) CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*types.RunnerScaleSetSession, error) {
	path := fmt.Sprintf("/%s/%d/sessions", scaleSetEndpoint, runnerScaleSetId)

	newSession := &types.RunnerScaleSetSession{
		OwnerName: owner,
	}

	return RequestJSON[types.RunnerScaleSetSession, types.RunnerScaleSetSession](ctx, client, http.MethodPost, path, newSession)
}

func (client *ActionsClient) RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) (*types.RunnerScaleSetSession, error) {
	path := fmt.Sprintf("/%s/%d/sessions/%s", scaleSetEndpoint, runnerScaleSetId, sessionId.String())

	return RequestJSON[types.RunnerScaleSetSession, types.RunnerScaleSetSession](ctx, client, http.MethodPatch, path, nil)
}

func (client *ActionsClient) DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) error {
	path := fmt.Sprintf("/%s/%d/sessions/%s", scaleSetEndpoint, runnerScaleSetId, sessionId.String())

	_, err := RequestJSON[any, any](ctx, client, http.MethodDelete, path, nil)

	return err
}
