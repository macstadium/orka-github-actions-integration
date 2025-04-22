// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	ghErrors "github.com/macstadium/orka-github-actions-integration/pkg/github/errors"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	retryablehttp "github.com/macstadium/orka-github-actions-integration/pkg/http"
)

func (client *ActionsClient) GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64) (*types.RunnerScaleSetMessage, error) {
	u, err := url.Parse(messageQueueUrl)
	if err != nil {
		return nil, err
	}

	if lastMessageId > 0 {
		q := u.Query()
		q.Set("lastMessageId", strconv.FormatInt(lastMessageId, 10))
		u.RawQuery = q.Encode()
	}

	response, err := sendMessageQueueRequest(ctx, u.String(), http.MethodGet, &retryablehttp.ClientTransport{
		Token:  messageQueueAccessToken,
		Accept: "application/json; api-version=6.0-preview",
	}, nil)
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	if response.StatusCode == http.StatusAccepted {
		return nil, nil
	}

	if response.StatusCode != http.StatusOK {
		return nil, parseMessageQueueResponse(response)
	}

	var message *types.RunnerScaleSetMessage
	err = json.NewDecoder(response.Body).Decode(&message)
	if err != nil {
		return nil, err
	}

	return message, nil
}

func (client *ActionsClient) DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error {
	u, err := url.Parse(messageQueueUrl)
	if err != nil {
		return err
	}

	u.Path = fmt.Sprintf("%s/%d", u.Path, messageId)

	response, err := sendMessageQueueRequest(ctx, u.String(), http.MethodDelete, &retryablehttp.ClientTransport{
		Token:       messageQueueAccessToken,
		ContentType: "application/json",
	}, nil)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		return parseMessageQueueResponse(response)
	}

	return nil
}

func sendMessageQueueRequest(ctx context.Context, url, httpMethod string, httpTransport *retryablehttp.ClientTransport, body io.Reader) (*http.Response, error) {
	httpClient, err := retryablehttp.NewClient(httpTransport)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, url, body)
	if err != nil {
		return nil, err
	}

	return httpClient.Do(req)
}

func parseMessageQueueResponse(response *http.Response) error {
	if response.StatusCode != http.StatusUnauthorized {
		return ParseActionsErrorFromResponse(response)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	return &ghErrors.MessageQueueTokenExpiredError{Message: string(trimByteOrderMark(body))}
}
