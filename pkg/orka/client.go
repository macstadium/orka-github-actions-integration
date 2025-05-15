package orka

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/exec"
)

type OrkaService interface {
	DeployVM(ctx context.Context, namePrefix, vmConfig string) (*OrkaVMDeployResponseModel, error)
	DeleteVM(ctx context.Context, name string) error
}

type OrkaClient struct {
	envData *env.Data
}

func (client *OrkaClient) DeployVM(ctx context.Context, namePrefix, vmConfig string) (*OrkaVMDeployResponseModel, error) {
	args := []string{"vm", "deploy", namePrefix, "--config", vmConfig, "--generate-name", "-o", "json", "--namespace", client.envData.OrkaNamespace}
	if client.envData.OrkaVMMetadata != "" {
		args = append(args, "--metadata", client.envData.OrkaVMMetadata)
	}

	res, err := exec.ExecJSONCommand[[]*OrkaVMDeployResponseModel]("orka3", args)
	if err != nil {
		return nil, err
	}

	return (*res)[0], nil
}

func (client *OrkaClient) DeleteVM(ctx context.Context, name string) error {
	out, err := exec.ExecStringCommand("orka3", []string{"vm", "delete", name, "--namespace", client.envData.OrkaNamespace})
	if out == fmt.Sprintf("Successfully deleted vm %s", name) {
		return nil
	}

	return err
}

func NewOrkaClient(envData *env.Data, ctx context.Context) (*OrkaClient, error) {
	// This request is designed to fail quickly if there is no connectivity to the cluster.
	// The orka3 user set-token operation may take up to ~1 minute to fail, which is excessive.
	client := &http.Client{
		Transport: &OrkaTransport{
			Token: envData.OrkaToken,
		},
		Timeout: time.Second * 1,
	}
	_, err := api.RequestJSON[any, any](ctx, client, http.MethodGet, fmt.Sprintf("%s/api/v1/cluster-info", envData.OrkaURL), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the Orka cluster: %s", err.Error())
	}

	_, err = exec.ExecStringCommand("orka3", []string{"config", "set", "--api-url", envData.OrkaURL})
	if err != nil {
		return nil, err
	}

	_, err = exec.ExecStringCommand("orka3", []string{"user", "set-token", envData.OrkaToken})
	if err != nil {
		return nil, err
	}

	// The purpose of this call is to check the permissions of the provided token.
	// If the command fails with an "Unauthorized" error, it indicates that the provided token is not valid.
	_, err = exec.ExecStringCommand("orka3", []string{"node", "list"})
	if err != nil {
		if strings.Contains(err.Error(), "Unauthorized") {
			return nil, fmt.Errorf("the provided token is not valid. Please provide a valid token")
		}

		return nil, err
	}

	return &OrkaClient{
		envData: envData,
	}, nil
}

type OrkaTransport struct {
	Token string
}

func (t *OrkaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", t.Token))
	return http.DefaultTransport.RoundTrip(req)
}
