package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

func RequestJSON[Req any, Res any](ctx context.Context, client *ActionsClient, method string, path string, body *Req) (*Res, error) {
	buffer := bytes.Buffer{}
	if body != nil {
		if err := json.NewEncoder(&buffer).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := client.newActionsServiceRequest(ctx, method, path, &buffer)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if response.StatusCode != http.StatusOK {
		return nil, ParseActionsErrorFromResponse(response)
	}

	responseModel := new(Res)
	if err = json.NewDecoder(response.Body).Decode(&responseModel); err != nil {
		return nil, err
	}

	return responseModel, nil
}
