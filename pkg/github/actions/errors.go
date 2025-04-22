// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ActionsError struct {
	ExceptionName string `json:"typeName,omitempty"`
	Message       string `json:"message,omitempty"`
	StatusCode    int
}

func (e *ActionsError) Error() string {
	return fmt.Sprintf("%v - had issue communicating with Actions backend: %v", e.StatusCode, e.Message)
}

func ParseActionsErrorFromResponse(response *http.Response) error {
	if response.ContentLength == 0 {
		message := "Request returned status: " + response.Status
		return &ActionsError{
			ExceptionName: "unknown",
			Message:       message,
			StatusCode:    response.StatusCode,
		}
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}

	body = trimByteOrderMark(body)
	contentType, ok := response.Header["Content-Type"]
	if ok && len(contentType) > 0 && strings.Contains(contentType[0], "text/plain") {
		message := string(body)
		statusCode := response.StatusCode
		return &ActionsError{
			Message:    message,
			StatusCode: statusCode,
		}
	}

	actionsError := &ActionsError{StatusCode: response.StatusCode}
	if err := json.Unmarshal(body, &actionsError); err != nil {
		return err
	}

	return actionsError
}

func trimByteOrderMark(body []byte) []byte {
	return bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
}
