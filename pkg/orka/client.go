package orka

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/api"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/exec"
)

// cliBinary is the Orka CLI executable. Orka 4.0 renamed it from orka3 to orka.
// The container image installs it as orka, with an orka3 symlink.
const cliBinary = "orka"

type OrkaService interface {
	DeployVM(ctx context.Context, namePrefix, vmConfig string) (*OrkaVMDeployResponseModel, error)
	DeleteVM(ctx context.Context, name string) error
	ListVMs(ctx context.Context, namePrefix string) ([]*OrkaVMInfo, error)

	DeployEmulator(ctx context.Context, name, vmName, config string) (*OrkaEmulatorResponseModel, error)
	DeleteEmulator(ctx context.Context, name string) error
	ListEmulators(ctx context.Context) ([]*OrkaEmulatorResponseModel, error)
	ListEmulatorConfigs(ctx context.Context) ([]*OrkaEmulatorConfigResponseModel, error)
	ListVMConfigs(ctx context.Context) ([]*OrkaVMConfigResponseModel, error)
}

type OrkaClient struct {
	envData *env.Data
}

func (client *OrkaClient) DeployVM(ctx context.Context, namePrefix, vmConfig string) (*OrkaVMDeployResponseModel, error) {
	args := []string{"vm", "deploy", namePrefix, "--config", vmConfig, "--generate-name", "-o", "json", "--namespace", client.envData.OrkaNamespace}
	if client.envData.OrkaVMMetadata != "" {
		args = append(args, "--metadata", client.envData.OrkaVMMetadata)
	}

	res, err := exec.ExecJSONCommand[[]*OrkaVMDeployResponseModel](ctx, cliBinary, args)
	if err != nil {
		return nil, err
	}

	return (*res)[0], nil
}

func (client *OrkaClient) ListVMs(ctx context.Context, namePrefix string) ([]*OrkaVMInfo, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaVMInfo](ctx, cliBinary, []string{"vm", "list", "--namespace", client.envData.OrkaNamespace, "-o", "json"})
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
// config. The config owns the platform, system image, and device profile, so this only passes a
// name through. The call blocks until the emulator is running, fails, or the timeout elapses,
// which can take minutes on a node that has not yet pulled the system image.
func (client *OrkaClient) DeployEmulator(ctx context.Context, name, vmName, config string) (*OrkaEmulatorResponseModel, error) {
	args := []string{
		"emulator", "deploy", name,
		"--vm", vmName,
		"--config", config,
		"--timeout", strconv.Itoa(client.envData.OrkaEmulatorDeployTimeout),
		"-o", "json",
		"--namespace", client.envData.OrkaNamespace,
	}

	res, err := exec.ExecJSONCommand[[]*OrkaEmulatorResponseModel](ctx, cliBinary, args)
	if err != nil {
		return nil, err
	}

	if res == nil || len(*res) == 0 {
		return nil, fmt.Errorf("orka reported no emulator for %s", name)
	}

	return (*res)[0], nil
}

// DeleteEmulator removes the named emulator. A missing emulator is treated as success: the operator
// garbage-collects emulators through the paired VM's owner reference, so by the time we get here it
// may already be gone. It deletes one emulator per call, so the outcome of each is known.
func (client *OrkaClient) DeleteEmulator(ctx context.Context, name string) error {
	_, err := exec.ExecStringCommand(ctx, cliBinary, []string{"emulator", "delete", name, "--namespace", client.envData.OrkaNamespace})
	if err != nil && isEmulatorNotFound(err, name) {
		return nil
	}

	return err
}

// isEmulatorNotFound reports whether err is the CLI's report that the named emulator does not exist,
// the raw Kubernetes NotFound error androidemulatorinstances.orka.macstadium.com "<name>" not found.
// It matches that wording for this name rather than any "not found", which would also swallow
// failures such as a missing CLI binary. The quotes are optionally escaped, because the exec package
// quotes the CLI's output into its error.
func isEmulatorNotFound(err error, name string) bool {
	pattern := `androidemulatorinstances\.orka\.macstadium\.com \\?"` + regexp.QuoteMeta(name) + `\\?" not found`
	return regexp.MustCompile(pattern).MatchString(err.Error())
}

func (client *OrkaClient) ListEmulators(ctx context.Context) ([]*OrkaEmulatorResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaEmulatorResponseModel](ctx, cliBinary, []string{"emulator", "list", "-o", "json", "--namespace", client.envData.OrkaNamespace})
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, nil
	}

	return *res, nil
}

// ListEmulatorConfigs lists every Android emulator config in the cluster. Emulator configs always
// live in orka-default and the command group has no --namespace flag, so none is passed.
func (client *OrkaClient) ListEmulatorConfigs(ctx context.Context) ([]*OrkaEmulatorConfigResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaEmulatorConfigResponseModel](ctx, cliBinary, []string{"emulator-config", "list", "-o", "json"})
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, nil
	}

	return *res, nil
}

// ListVMConfigs lists every VM config in the cluster. Like emulator configs, VM configs always live
// in orka-default, so no namespace is passed.
func (client *OrkaClient) ListVMConfigs(ctx context.Context) ([]*OrkaVMConfigResponseModel, error) {
	res, err := exec.ExecJSONCommand[[]*OrkaVMConfigResponseModel](ctx, cliBinary, []string{"vm-config", "list", "-o", "json"})
	if err != nil {
		return nil, err
	}

	if res == nil {
		return nil, nil
	}

	return *res, nil
}

// EmulatorsForVM returns the emulators paired with exactly vmName. Matching is exact rather than by
// prefix: VM foo-abcde must never select the emulators of foo-abcde2 or of a VM in another scale set
// whose name happens to start the same way.
func EmulatorsForVM(emulators []*OrkaEmulatorResponseModel, vmName string) []*OrkaEmulatorResponseModel {
	var paired []*OrkaEmulatorResponseModel
	for _, emulator := range emulators {
		if emulator != nil && emulator.VM == vmName {
			paired = append(paired, emulator)
		}
	}

	return paired
}

// BelongsToScaleSet reports whether vmName was generated for the scale set. Generated names are the
// scale set name, a dash, and a Kubernetes generated suffix with no dashes in it, so scale set foo
// owns foo-abcde but not foo-bar-abcde, which belongs to scale set foo-bar.
func BelongsToScaleSet(vmName, scaleSetName string) bool {
	suffix, found := strings.CutPrefix(vmName, scaleSetName+"-")
	return found && suffix != "" && !strings.Contains(suffix, "-")
}

func (client *OrkaClient) DeleteVM(ctx context.Context, name string) error {
	out, err := exec.ExecStringCommand(ctx, cliBinary, []string{"vm", "delete", name, "--namespace", client.envData.OrkaNamespace})
	if out == fmt.Sprintf("Successfully deleted vm %s", name) {
		return nil
	}

	return err
}

func NewOrkaClient(envData *env.Data, ctx context.Context) (*OrkaClient, error) {
	// This request is designed to fail quickly if there is no connectivity to the cluster.
	// The orka user set-token operation may take up to ~1 minute to fail, which is excessive.
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

	_, err = exec.ExecStringCommand(ctx, cliBinary, []string{"config", "set", "--api-url", envData.OrkaURL})
	if err != nil {
		return nil, err
	}

	_, err = exec.ExecStringCommand(ctx, cliBinary, []string{"user", "set-token", envData.OrkaToken})
	if err != nil {
		return nil, err
	}

	// The purpose of this call is to check the permissions of the provided token.
	// If the command fails with an "Unauthorized" error, it indicates that the provided token is not valid.
	_, err = exec.ExecStringCommand(ctx, cliBinary, []string{"node", "list", "--namespace", envData.OrkaNamespace})
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
