package orka

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/exec"
)

type OrkaService interface {
	DeployVM(ctx context.Context, namePrefix, vmConfig string) (*OrkaVMDeployResponseModel, error)
	DeleteVM(ctx context.Context, name string) error
	ListVMs(ctx context.Context, namePrefix string) ([]*OrkaVMInfo, error)
}

type OrkaClient struct {
	envData *env.Data
	// userdataPath is a private file holding the userdata script. The file
	// path (not the script content) is passed to orka3, which reads and
	// base64-encodes it, so the script never appears in command lines or logs.
	userdataPath string
}

func (client *OrkaClient) DeployVM(ctx context.Context, namePrefix, vmConfig string) (*OrkaVMDeployResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaVMDeployResponseModel]("orka3", client.deployArgs(namePrefix, vmConfig))
	if err != nil {
		return nil, err
	}

	return (*res)[0], nil
}

func (client *OrkaClient) deployArgs(namePrefix, vmConfig string) []string {
	args := []string{"vm", "deploy", namePrefix, "--config", vmConfig, "--generate-name", "-o", "json", "--namespace", client.envData.OrkaNamespace}
	if client.envData.OrkaVMMetadata != "" {
		args = append(args, "--metadata", client.envData.OrkaVMMetadata)
	}
	if client.userdataPath != "" {
		args = append(args, "--userdata", client.userdataPath)
	}

	return args
}

func (client *OrkaClient) ListVMs(ctx context.Context, namePrefix string) ([]*OrkaVMInfo, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaVMInfo]("orka3", []string{"vm", "list", "--namespace", client.envData.OrkaNamespace, "-o", "json"})
	if err != nil {
		return nil, err
	}

	var filtered []*OrkaVMInfo
	for _, vm := range *res {
		if strings.HasPrefix(vm.Name, namePrefix) {
			filtered = append(filtered, vm)
		}
	}
	return filtered, nil
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
	_, err = exec.ExecStringCommand("orka3", []string{"node", "list", "--namespace", envData.OrkaNamespace})
	if err != nil {
		if strings.Contains(err.Error(), "Unauthorized") {
			return nil, fmt.Errorf("the provided token is not valid. Please provide a valid token")
		}

		return nil, err
	}

	userdataPath := ""
	if envData.OrkaVMUserdata != "" {
		deployHelp, err := exec.ExecStringCommand("orka3", []string{"vm", "deploy", "--help"})
		if err != nil || !strings.Contains(deployHelp, "--userdata") {
			return nil, fmt.Errorf("the installed orka3 CLI does not support the --userdata flag. Userdata requires orka3 3.7.0 or later; upgrade the CLI or unset %s", env.OrkaVMUserdataEnvName)
		}

		if userdataPath, err = writeUserdataFile(envData.OrkaVMUserdata); err != nil {
			return nil, fmt.Errorf("failed to prepare the userdata script: %s", err.Error())
		}
	}

	return &OrkaClient{
		envData:      envData,
		userdataPath: userdataPath,
	}, nil
}

// writeUserdataFile stores the userdata script in a file readable only by the
// current user, so deploys can reference it by path instead of passing the
// script content on the orka3 command line.
func writeUserdataFile(script string) (string, error) {
	file, err := os.CreateTemp("", "orka-vm-userdata-*.sh")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(script); err != nil {
		return "", err
	}

	return file.Name(), nil
}

type OrkaTransport struct {
	Token string
}

func (t *OrkaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", t.Token))
	return http.DefaultTransport.RoundTrip(req)
}
