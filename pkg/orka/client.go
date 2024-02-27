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
	DeployVM(ctx context.Context, vmName, vmConfig string) (*OrkaVMDeployResponseModel, error)
	DeleteVM(ctx context.Context, name string) error
	GetVMConfig(ctx context.Context, name string) (*OrkaVMConfigResponseModel, error)
	GetImage(ctx context.Context, name string) (*OrkaImageResponseModel, error)
}

type OrkaClient struct {
	envData *env.Data
}

func (client *OrkaClient) DeployVM(ctx context.Context, vmName, vmConfig string) (*OrkaVMDeployResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaVMDeployResponseModel]("orka3", []string{"vm", "deploy", vmName, "--config", vmConfig, "-o", "json", "--namespace", client.envData.OrkaNamespace})
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

func (client *OrkaClient) GetVMConfig(ctx context.Context, name string) (*OrkaVMConfigResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaVMConfigResponseModel]("orka3", []string{"vm-config", "list", "-o", "json"})
	if err != nil {
		return nil, err
	}

	for _, config := range *res {
		if config.Name == name {
			return config, nil
		}
	}

	return nil, fmt.Errorf("failed to get VM config %s", name)
}

func (client *OrkaClient) GetImage(ctx context.Context, name string) (*OrkaImageResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaImageResponseModel]("orka3", []string{"image", "list", "-o", "json"})
	if err != nil {
		return nil, err
	}

	for _, image := range *res {
		if image.Name == name {
			return image, nil
		}
	}

	return nil, fmt.Errorf("failed to get image %s", name)
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
