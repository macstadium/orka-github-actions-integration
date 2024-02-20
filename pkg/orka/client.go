package orka

import (
	"context"
	"fmt"

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
	out, err := exec.ExecStringCommand("orka3", []string{"vm", "delete", name})
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

	return nil, nil
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

	return nil, nil
}

func NewOrkaClient(envData *env.Data, ctx context.Context) (*OrkaClient, error) {
	_, err := exec.ExecStringCommand("orka3", []string{"config", "set", "--api-url", envData.OrkaURL})
	if err != nil {
		return nil, err
	}

	_, err = exec.ExecStringCommand("orka3", []string{"user", "set-token", envData.OrkaToken})
	if err != nil {
		return nil, err
	}

	return &OrkaClient{
		envData: envData,
	}, nil
}
