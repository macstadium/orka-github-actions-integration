package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func RequestJSON[Req any, Res any](ctx context.Context, client *http.Client, method string, path string, body *Req) (*Res, error) {
	buffer := bytes.Buffer{}
	if body != nil {
		if err := json.NewEncoder(&buffer).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, path, &buffer)
	if err != nil {
		return nil, err
	}

	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	isSuccessStatusCode := response.StatusCode >= 200 && response.StatusCode <= 299
	if !isSuccessStatusCode {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("%s", string(body))
	}

	responseModel := new(Res)
	if err = json.NewDecoder(response.Body).Decode(&responseModel); err != nil {
		return nil, err
	}

	return responseModel, nil
}
