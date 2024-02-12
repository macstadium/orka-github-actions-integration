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
