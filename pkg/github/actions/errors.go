// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package actions

import (
	"fmt"
)

type ActionsError struct {
	ExceptionName string `json:"typeName,omitempty"`
	Message       string `json:"message,omitempty"`
	StatusCode    int
}

func (e *ActionsError) Error() string {
	return fmt.Sprintf("%v - had issue communicating with Actions backend: %v", e.StatusCode, e.Message)
}
