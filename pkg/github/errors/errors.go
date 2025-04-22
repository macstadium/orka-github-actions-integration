// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package errors

type MessageQueueTokenExpiredError struct {
	Message string
}

func (e *MessageQueueTokenExpiredError) Error() string {
	return e.Message
}

type HttpClientSideError struct {
	msg  string
	Code int
}

func (e *HttpClientSideError) Error() string {
	return e.msg
}
