package retryablehttp

import (
	"fmt"
	"net/http"
	"time"

	retryable "github.com/hashicorp/go-retryablehttp"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
)

type Client struct {
	*http.Client

	retryMax     int
	retryWaitMax time.Duration
}

type ClientTransport struct {
	Token       string
	ContentType string
	RemoteAuth  string
	Accept      string
}

func (t *ClientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	authorization := fmt.Sprintf("Bearer %s", t.Token)

	if t.RemoteAuth != "" {
		authorization = fmt.Sprintf("RemoteAuth %s", t.RemoteAuth)
	}

	req.Header.Set("Authorization", authorization)
	req.Header.Set("Content-Type", t.ContentType)

	if t.Accept != "" {
		req.Header.Set("Accept", t.Accept)
	}

	return http.DefaultTransport.RoundTrip(req)
}

func NewClient(transport *ClientTransport) (*Client, error) {
	client := &Client{
		retryMax:     4,
		retryWaitMax: 30 * time.Second,
	}

	retryClient := retryable.NewClient()
	retryClient.Logger = &LeveledLogger{logger: logging.Logger}

	retryClient.RetryMax = client.retryMax
	retryClient.RetryWaitMax = client.retryWaitMax
	retryClient.HTTPClient.Timeout = 5 * time.Minute

	retryClient.HTTPClient.Transport = transport
	client.Client = retryClient.StandardClient()

	return client, nil
}
