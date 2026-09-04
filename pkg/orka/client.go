package orka

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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

	DeployEmulator(ctx context.Context, name, vmName, emulatorConfig string) (*OrkaEmulatorResponseModel, error)
	DeleteEmulator(ctx context.Context, names ...string) error
	ListEmulators(ctx context.Context) ([]*OrkaEmulatorResponseModel, error)
	ListEmulatorConfigs(ctx context.Context) ([]*OrkaEmulatorConfigResponseModel, error)
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

// DeployEmulator deploys an Android emulator paired with a running VM, from a named emulator
// config. The config owns the platform, system image, device profile, and sizing, so this only
// passes a name through. The call blocks until the emulator is running, fails, or the timeout
// elapses, which can take minutes on a node that has not yet pulled the system image.
func (client *OrkaClient) DeployEmulator(ctx context.Context, name, vmName, emulatorConfig string) (*OrkaEmulatorResponseModel, error) {
	args := []string{
		"emulator", "deploy", name,
		"--vm", vmName,
		"--config", emulatorConfig,
		"--timeout", strconv.Itoa(client.envData.OrkaEmulatorDeployTimeout),
		"-o", "json",
		"--namespace", client.envData.OrkaNamespace,
	}

	res, err := exec.ExecJSONCommand[[]*OrkaEmulatorResponseModel]("orka3", args)
	if err != nil {
		return nil, err
	}

	if res == nil || len(*res) == 0 {
		return nil, fmt.Errorf("orka3 reported no emulator for %s", name)
	}

	return (*res)[0], nil
}

// DeleteEmulator removes the named emulators. A missing emulator is treated as success: the
// operator garbage-collects emulators through the paired VM's owner reference, so by the time we
// get here one may already be gone.
func (client *OrkaClient) DeleteEmulator(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return nil
	}

	args := append([]string{"emulator", "delete"}, names...)
	args = append(args, "--namespace", client.envData.OrkaNamespace)

	_, err := exec.ExecStringCommand("orka3", args)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "not found") {
		return nil
	}

	return err
}

func (client *OrkaClient) ListEmulators(ctx context.Context) ([]*OrkaEmulatorResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaEmulatorResponseModel]("orka3", []string{"emulator", "list", "-o", "json", "--namespace", client.envData.OrkaNamespace})
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, nil
	}

	return *res, nil
}

func (client *OrkaClient) ListEmulatorConfigs(ctx context.Context) ([]*OrkaEmulatorConfigResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaEmulatorConfigResponseModel]("orka3", []string{"emulator-config", "list", "-o", "json", "--namespace", client.envData.OrkaNamespace})
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, nil
	}

	return *res, nil
}

// verifyEmulatorConfigs fails startup when the cluster cannot serve emulators at all, or when a
// configured emulator config does not exist. Checking here rather than at deploy time is the point
// of naming configs instead of inlining specs: a typo becomes one startup error instead of a
// failure on every job, minutes into each provisioning attempt.
func (client *OrkaClient) verifyEmulatorConfigs(ctx context.Context) error {
	configs, err := client.ListEmulatorConfigs(ctx)
	if err != nil {
		return fmt.Errorf("unable to list Android emulator configs, which %s requires. The cluster and the bundled orka3 CLI must both support Android emulators. More info: %s", env.OrkaEmulatorConfigsEnvName, err.Error())
	}

	missing, available := diffEmulatorConfigs(client.envData.OrkaEmulatorConfigs, configs)
	if len(missing) == 0 {
		return nil
	}

	hint := "no emulator configs exist in this namespace"
	if len(available) > 0 {
		hint = fmt.Sprintf("available: %s", strings.Join(available, ", "))
	}

	return fmt.Errorf("%s names Android emulator config(s) that do not exist in namespace %s: %s (%s)", env.OrkaEmulatorConfigsEnvName, client.envData.OrkaNamespace, strings.Join(missing, ", "), hint)
}

// diffEmulatorConfigs returns the configured names absent from the cluster, and the names that do
// exist, preserving the order each was given in.
func diffEmulatorConfigs(configured []string, existing []*OrkaEmulatorConfigResponseModel) (missing []string, available []string) {
	known := make(map[string]bool, len(existing))
	available = make([]string, 0, len(existing))
	for _, config := range existing {
		known[config.Name] = true
		available = append(available, config.Name)
	}

	missing = make([]string, 0, len(configured))
	for _, name := range configured {
		if !known[name] {
			missing = append(missing, name)
		}
	}

	return missing, available
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

	orkaClient := &OrkaClient{
		envData: envData,
	}

	if envData.EmulatorsEnabled() {
		if err := orkaClient.verifyEmulatorConfigs(ctx); err != nil {
			return nil, err
		}
	}

	return orkaClient, nil
}

type OrkaTransport struct {
	Token string
}

func (t *OrkaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", t.Token))
	return http.DefaultTransport.RoundTrip(req)
}
