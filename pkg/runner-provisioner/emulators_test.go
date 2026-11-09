package provisioner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/emulator"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/metrics"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeOrka records the emulator and VM calls the provisioner makes. Deploys run concurrently, so
// every field is guarded by mu. Like the real CLI, the emulator list and delete fail once their
// context is done, so a test can tell whether cleanup ran under a context that outlives the attempt.
type fakeOrka struct {
	mu sync.Mutex

	deployEmulatorFunc func(ctx context.Context, name, vmName, config string) (*orka.OrkaEmulatorResponseModel, error)
	listEmulatorsFunc  func(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error)
	// duringVMDeploy runs while the VM deploy is in flight, after the VM exists.
	duringVMDeploy func()

	deployedEmulators map[string]string // emulator name -> config
	deletedEmulators  []string
	deletedVMs        []string
	listEmulatorCalls int
}

func newFakeOrka() *fakeOrka {
	return &fakeOrka{deployedEmulators: map[string]string{}}
}

func (f *fakeOrka) DeployVM(ctx context.Context, namePrefix, vmConfig string) (*orka.OrkaVMDeployResponseModel, error) {
	if f.duringVMDeploy != nil {
		f.duringVMDeploy()
	}

	// The CLI is killed if its context is done before the deploy returns, and the VM is left behind.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	ssh := 8822
	return &orka.OrkaVMDeployResponseModel{Name: namePrefix + "-abcde", Node: "mini-arm-36", IP: "10.221.190.36", SSH: &ssh, Status: orka.VMRunning}, nil
}

func (f *fakeOrka) DeleteVM(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedVMs = append(f.deletedVMs, name)
	return nil
}

func (f *fakeOrka) ListVMs(ctx context.Context, namePrefix string) ([]*orka.OrkaVMInfo, error) {
	return nil, nil
}

func (f *fakeOrka) DeployEmulator(ctx context.Context, name, vmName, config string) (*orka.OrkaEmulatorResponseModel, error) {
	f.mu.Lock()
	f.deployedEmulators[name] = config
	deploy := f.deployEmulatorFunc
	f.mu.Unlock()

	if deploy != nil {
		return deploy(ctx, name, vmName, config)
	}

	return runningEmulator(name, vmName, 15555), nil
}

func (f *fakeOrka) DeleteEmulator(ctx context.Context, names ...string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedEmulators = append(f.deletedEmulators, names...)
	return nil
}

func (f *fakeOrka) ListEmulators(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error) {
	f.mu.Lock()
	f.listEmulatorCalls++
	list := f.listEmulatorsFunc
	f.mu.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if list != nil {
		return list(ctx)
	}

	return nil, nil
}

func (f *fakeOrka) ListEmulatorConfigs(ctx context.Context) ([]*orka.OrkaEmulatorConfigResponseModel, error) {
	return nil, nil
}

func (f *fakeOrka) ListVMConfigs(ctx context.Context) ([]*orka.OrkaVMConfigResponseModel, error) {
	return nil, nil
}

func (f *fakeOrka) deployCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deployedEmulators)
}

func (f *fakeOrka) sortedDeletedEmulators() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.deletedEmulators...)
	sort.Strings(out)
	return out
}

type fakeEmulatorSource struct {
	supported bool
	refs      map[string][]emulator.EmulatorRef
}

func (f *fakeEmulatorSource) Supported() bool { return f.supported }

func (f *fakeEmulatorSource) Emulators(vmConfig string) []emulator.EmulatorRef {
	return f.refs[vmConfig]
}

func runningEmulator(name, vmName string, port int) *orka.OrkaEmulatorResponseModel {
	host := "192.168.64.1"
	return &orka.OrkaEmulatorResponseModel{
		Name:      name,
		VM:        vmName,
		Status:    orka.EmulatorRunning,
		Platform:  "android-36",
		ImageType: "google_apis",
		RelayIP:   &host,
		RelayPort: &port,
	}
}

func relayEmulator(name, config, suffix, host string, port int, deviceProfile *string) provisionedEmulator {
	return provisionedEmulator{
		ref: emulator.EmulatorRef{Config: config, Suffix: suffix},
		emulator: &orka.OrkaEmulatorResponseModel{
			Name:          name,
			Status:        orka.EmulatorRunning,
			Platform:      "android-36",
			ImageType:     "google_apis",
			DeviceProfile: deviceProfile,
			RelayIP:       &host,
			RelayPort:     &port,
		},
	}
}

func exportsOf(emulators ...provisionedEmulator) ([]string, []droppedAlias) {
	return buildEmulatorExports(emulators)
}

var _ = Describe("Provisioning with emulators", func() {
	const vmConfig = "foo"
	const vmName = "foo-abcde"

	var (
		fake        *fakeOrka
		source      *fakeEmulatorSource
		mockActions *MockActionsService
		p           *RunnerProvisioner
		ctx         context.Context
	)

	BeforeEach(func() {
		logging.SetupLogger("info")
		ctx = context.Background()
		fake = newFakeOrka()
		source = &fakeEmulatorSource{supported: true, refs: map[string][]emulator.EmulatorRef{}}
		mockActions = &MockActionsService{
			CreateRunnerFunc: func(ctx context.Context, id int, name string) (*types.RunnerScaleSetJitRunnerConfig, error) {
				return &types.RunnerScaleSetJitRunnerConfig{EncodedJITConfig: "jit"}, nil
			},
		}

		envData := &env.Data{
			OrkaVMConfig:                     vmConfig,
			OrkaVMUsername:                   "admin",
			GitHubRunnerVersion:              "2.330.0",
			OrkaEmulatorDeployTimeout:        10,
			RunnerDeregistrationTimeout:      time.Second,
			RunnerDeregistrationPollInterval: 10 * time.Millisecond,
		}

		p = NewRunnerProvisioner(&types.RunnerScaleSet{Id: 1, Name: "foo"}, mockActions, fake, envData, source, metrics.New("foo"))
	})

	It("runs exactly today's commands with no emulator deploys when nothing is paired", func() {
		executor, commands, err := p.ProvisionRunner(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(executor.VMName).To(Equal(vmName))

		Expect(commands).To(Equal(buildCommands("jit", "2.330.0", "admin", nil)))
		Expect(commands).To(HaveLen(len(commands_template)))
		Expect(commands[0]).To(Equal("set -e"))
		Expect(fake.deployCount()).To(Equal(0))
	})

	It("behaves the same with no emulator source at all", func() {
		p.emulators = nil

		_, commands, err := p.ProvisionRunner(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(commands).To(HaveLen(len(commands_template)))
		Expect(fake.deployCount()).To(Equal(0))
	})

	It("deploys every paired emulator under deterministic names and exports them ahead of the runner", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{
			{Config: "foo-emulator", Suffix: ""},
			{Config: "foo-emulator-tablet", Suffix: "tablet"},
		}

		_, commands, err := p.ProvisionRunner(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(fake.deployedEmulators).To(Equal(map[string]string{
			"foo-abcde-emu-1": "foo-emulator",
			"foo-abcde-emu-2": "foo-emulator-tablet",
		}))
		Expect(commands[0]).To(Equal("export ORKA_EMULATOR_COUNT='2'"))
		Expect(commands).To(ContainElements(
			"export ORKA_EMULATOR_1_CONFIG='foo-emulator'",
			"export ORKA_EMULATOR_2_CONFIG='foo-emulator-tablet'",
			"export ORKA_EMULATOR_TABLET_NAME='foo-abcde-emu-2'",
		))
		Expect(commands).To(ContainElement("set -e"))
		Expect(fake.deletedVMs).To(BeEmpty())
	})

	It("tears down every emulator and the VM when one of several deploys fails", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{
			{Config: "foo-emulator", Suffix: ""},
			{Config: "foo-emulator-tablet", Suffix: "tablet"},
			{Config: "foo-emulator-wear", Suffix: "wear"},
		}
		fake.deployEmulatorFunc = func(ctx context.Context, name, vm, config string) (*orka.OrkaEmulatorResponseModel, error) {
			if config == "foo-emulator-tablet" {
				return nil, errors.New("boom")
			}
			return runningEmulator(name, vm, 15555), nil
		}
		fake.listEmulatorsFunc = func(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error) {
			return []*orka.OrkaEmulatorResponseModel{
				runningEmulator("foo-abcde-emu-1", vmName, 15555),
				runningEmulator("foo-abcde-emu-3", vmName, 15557),
				runningEmulator("foo-bar-abcde-emu-1", "foo-bar-abcde", 15559),
			}, nil
		}

		_, _, err := p.ProvisionRunner(ctx)
		Expect(err).To(MatchError("1 of 3 emulator(s) paired with VM foo-abcde failed to deploy on node mini-arm-36 (1 failed for other reasons): emulator foo-abcde-emu-2 (foo-emulator-tablet): boom"))

		Expect(fake.deployCount()).To(Equal(3))
		Expect(fake.sortedDeletedEmulators()).To(Equal([]string{"foo-abcde-emu-1", "foo-abcde-emu-3"}))
		Expect(fake.deletedVMs).To(Equal([]string{vmName}))
		Expect(mockActions.CreateRunnerCalls).To(Equal(0))
	})

	It("treats a running emulator without a relay address as a failure", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{{Config: "foo-emulator"}}
		fake.deployEmulatorFunc = func(ctx context.Context, name, vm, config string) (*orka.OrkaEmulatorResponseModel, error) {
			return &orka.OrkaEmulatorResponseModel{Name: name, VM: vm, Status: orka.EmulatorRunning}, nil
		}

		_, _, err := p.ProvisionRunner(ctx)
		Expect(err).To(MatchError(ContainSubstring("no relay address")))
		Expect(fake.deletedVMs).To(Equal([]string{vmName}))
	})

	It("marks a full node as a capacity failure", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{{Config: "foo-emulator"}}
		fake.deployEmulatorFunc = func(ctx context.Context, name, vm, config string) (*orka.OrkaEmulatorResponseModel, error) {
			return nil, errors.New(`Error: failed to create AVD "orka-default-foo-abcde-emu-1-fdbb6b5e" for VM "orka-default-foo-abcde" with platform "android-36", image type "google_apis" and device profile "": rpc error: code = DeadlineExceeded desc = context deadline exceeded`)
		}

		_, _, err := p.ProvisionRunner(ctx)
		Expect(errors.Is(err, ErrEmulatorCapacity)).To(BeTrue())
		Expect(fake.deletedVMs).To(Equal([]string{vmName}))
	})

	It("reports a full node on one line however many emulators fail", func() {
		for i := range 20 {
			suffix := string(rune('a' + i))
			source.refs[vmConfig] = append(source.refs[vmConfig], emulator.EmulatorRef{Config: "foo-emulator-" + suffix, Suffix: suffix})
		}
		fake.deployEmulatorFunc = func(ctx context.Context, name, vm, config string) (*orka.OrkaEmulatorResponseModel, error) {
			return nil, fmt.Errorf(`Error: failed to create AVD "orka-default-%s" for VM "orka-default-%s": rpc error: code = DeadlineExceeded desc = context deadline exceeded`, name, vm)
		}

		_, _, err := p.ProvisionRunner(ctx)

		Expect(err.Error()).ToNot(ContainSubstring("\n"))
		Expect(err.Error()).To(HavePrefix("20 of 20 emulator(s) paired with VM foo-abcde failed to deploy on node mini-arm-36 (20 had no room on the node): emulator foo-abcde-emu-1 (foo-emulator-a): insufficient Android emulator capacity: "))
		Expect(strings.Count(err.Error(), "failed to create AVD")).To(Equal(1))
		Expect(errors.Is(err, ErrEmulatorCapacity)).To(BeTrue())
	})

	It("quotes a failure that is not about capacity when a burst fails both ways", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{
			{Config: "foo-emulator", Suffix: ""},
			{Config: "foo-emulator-tablet", Suffix: "tablet"},
			{Config: "foo-emulator-wear", Suffix: "wear"},
		}
		fake.deployEmulatorFunc = func(ctx context.Context, name, vm, config string) (*orka.OrkaEmulatorResponseModel, error) {
			if config == "foo-emulator-wear" {
				return nil, errors.New(`Error: androidemulatorconfigs.orka.macstadium.com "foo-emulator-wear" not found`)
			}
			return nil, errors.New("0/3 nodes are available: 1 Insufficient orka.macstadium.com/emulator")
		}

		_, _, err := p.ProvisionRunner(ctx)

		Expect(err).To(MatchError(`3 of 3 emulator(s) paired with VM foo-abcde failed to deploy on node mini-arm-36 (2 had no room on the node, 1 failed for other reasons): emulator foo-abcde-emu-3 (foo-emulator-wear): Error: androidemulatorconfigs.orka.macstadium.com "foo-emulator-wear" not found`))
		Expect(errors.Is(err, ErrEmulatorCapacity)).To(BeTrue())
	})

	It("kills an in-flight deploy when the context is cancelled and still cleans up", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{{Config: "foo-emulator"}}
		attemptCtx, cancel := context.WithCancel(ctx)
		started := make(chan struct{})
		fake.deployEmulatorFunc = func(ctx context.Context, name, vm, config string) (*orka.OrkaEmulatorResponseModel, error) {
			close(started)
			<-ctx.Done()
			return nil, fmt.Errorf("command was interrupted: %w", ctx.Err())
		}
		// The interrupted deploy leaves its record behind, and cleanup must still find and delete it
		// although the attempt's context is done.
		fake.listEmulatorsFunc = func(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error) {
			return []*orka.OrkaEmulatorResponseModel{runningEmulator("foo-abcde-emu-1", vmName, 15555)}, nil
		}
		go func() {
			<-started
			cancel()
		}()

		_, _, err := p.ProvisionRunner(attemptCtx)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(fake.sortedDeletedEmulators()).To(Equal([]string{"foo-abcde-emu-1"}))
		Expect(fake.deletedVMs).To(Equal([]string{vmName}))
	})

	It("lets a cancelled VM deploy finish, then deletes the VM", func() {
		source.refs[vmConfig] = []emulator.EmulatorRef{{Config: "foo-emulator"}}
		attemptCtx, cancel := context.WithCancel(ctx)
		fake.duringVMDeploy = cancel

		_, _, err := p.ProvisionRunner(attemptCtx)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(fake.deployCount()).To(Equal(0))
		Expect(fake.deletedVMs).To(Equal([]string{vmName}))
		Expect(mockActions.CreateRunnerCalls).To(Equal(0))
	})

	Describe("cleanup", func() {
		It("deletes the emulators found on the VM by exact name, with no memory of the deploy", func() {
			p.emulators = nil
			fake.listEmulatorsFunc = func(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error) {
				return []*orka.OrkaEmulatorResponseModel{
					runningEmulator("foo-abcde-emu-1", vmName, 15555),
					runningEmulator("foo-abcde-emu-2", vmName, 15557),
					runningEmulator("foo-abcde2-emu-1", "foo-abcde2", 15559),
					runningEmulator("foo-bar-abcde-emu-1", "foo-bar-abcde", 15561),
				}, nil
			}

			p.CleanupResources(ctx, vmName)

			Expect(fake.sortedDeletedEmulators()).To(Equal([]string{"foo-abcde-emu-1", "foo-abcde-emu-2"}))
			Expect(fake.deletedVMs).To(Equal([]string{vmName}))
		})

		It("still deletes the VM when the emulator list fails", func() {
			fake.listEmulatorsFunc = func(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error) {
				return nil, errors.New("unknown command \"emulator\"")
			}

			p.CleanupResources(ctx, vmName)

			Expect(fake.deletedEmulators).To(BeEmpty())
			Expect(fake.deletedVMs).To(Equal([]string{vmName}))
		})

		It("deletes nothing extra when the VM has no emulators", func() {
			p.CleanupResources(ctx, vmName)

			Expect(fake.listEmulatorCalls).To(Equal(1))
			Expect(fake.deletedEmulators).To(BeEmpty())
			Expect(fake.deletedVMs).To(Equal([]string{vmName}))
		})
	})
})

var _ = Describe("Classifying emulator deploy failures", func() {
	DescribeTable("by the error the CLI returned",
		func(message, expected string) {
			Expect(classifyDeployError(errors.New(message))).To(Equal(expected))
		},
		Entry("the engine timing out on a full node",
			`Error: failed to create AVD "orka-default-x-emu-17-fdbb6b5e" for VM "orka-default-x" with platform "android-36", image type "google_apis" and device profile "": rpc error: code = DeadlineExceeded desc = context deadline exceeded`,
			metrics.EmulatorDeployCapacity),
		Entry("the scheduler refusing the pod",
			"0/3 nodes are available: 1 Insufficient orka.macstadium.com/emulator, 2 node(s) didn't match pod affinity rules.",
			metrics.EmulatorDeployCapacity),
		Entry("an AVD creation failure for another reason",
			`Error: failed to create AVD "x" for VM "y": rpc error: code = InvalidArgument desc = bad platform`,
			metrics.EmulatorDeployError),
		Entry("the CLI's own deploy timeout",
			"Error: deployment timed out after 10 minutes",
			metrics.EmulatorDeployError),
		Entry("a missing config",
			`Error: androidemulatorconfigs.orka.macstadium.com "foo-emulator" not found`,
			metrics.EmulatorDeployError),
	)
})

var _ = Describe("Emulator naming", func() {
	DescribeTable("derives a deterministic name from the VM and list position",
		func(vmName string, index int, expected string) {
			Expect(emulatorName(vmName, index)).To(Equal(expected))
		},
		Entry("first emulator is 1-indexed", "my-runner-abc123", 0, "my-runner-abc123-emu-1"),
		Entry("second emulator follows the list order", "my-runner-abc123", 1, "my-runner-abc123-emu-2"),
		Entry("tenth emulator is not zero padded", "my-runner-abc123", 9, "my-runner-abc123-emu-10"),
	)
})

var _ = Describe("Emulator exports", func() {
	It("emits nothing when no emulators were provisioned", func() {
		exports, dropped := buildEmulatorExports(nil)
		Expect(exports).To(BeNil())
		Expect(dropped).To(BeNil())
	})

	It("prepends exports ahead of the runner commands", func() {
		exports, _ := exportsOf(relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, nil))
		commands := buildCommands("jit", "2.336.0", "admin", exports)

		Expect(commands[0]).To(Equal("export ORKA_EMULATOR_COUNT='1'"))
		Expect(commands).To(HaveLen(len(exports) + len(commands_template)))
	})

	It("exports the indexed variables and the single-emulator aliases", func() {
		profile := "pixel_8"
		exports, _ := exportsOf(relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, &profile))

		Expect(exports).To(ContainElements(
			"export ORKA_EMULATOR_COUNT='1'",
			"export ORKA_EMULATOR_1_NAME='vm-emu-1'",
			"export ORKA_EMULATOR_1_CONFIG='foo-emulator'",
			"export ORKA_EMULATOR_1_ADB_HOST='192.168.64.1'",
			"export ORKA_EMULATOR_1_ADB_PORT='15555'",
			"export ORKA_EMULATOR_1_ADB='192.168.64.1:15555'",
			"export ORKA_EMULATOR_1_PLATFORM='android-36'",
			"export ORKA_EMULATOR_1_IMAGE_TYPE='google_apis'",
			"export ORKA_EMULATOR_1_DEVICE_PROFILE='pixel_8'",
			"export ORKA_EMULATOR_ADB_HOST='192.168.64.1'",
			"export ORKA_EMULATOR_ADB_PORT='15555'",
			"export ORKA_EMULATOR_ADB='192.168.64.1:15555'",
			"export ORKA_EMULATOR_ADB_TARGETS='192.168.64.1:15555'",
		))
	})

	It("exports an empty device profile when the config did not set one", func() {
		exports, _ := exportsOf(relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, nil))
		Expect(exports).To(ContainElement("export ORKA_EMULATOR_1_DEVICE_PROFILE=''"))
	})

	It("points the unprefixed aliases at the first emulator and lists every target", func() {
		exports, _ := exportsOf(
			relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, nil),
			relayEmulator("vm-emu-2", "foo-emulator-tablet", "tablet", "192.168.64.1", 15557, nil),
		)

		Expect(exports).To(ContainElements(
			"export ORKA_EMULATOR_COUNT='2'",
			"export ORKA_EMULATOR_2_ADB='192.168.64.1:15557'",
			"export ORKA_EMULATOR_2_CONFIG='foo-emulator-tablet'",
			"export ORKA_EMULATOR_ADB='192.168.64.1:15555'",
			"export ORKA_EMULATOR_ADB_TARGETS='192.168.64.1:15555,192.168.64.1:15557'",
		))
	})

	It("quotes values so a cluster-supplied string cannot break out of the command stream", func() {
		odd := "pixel'8"
		exports, _ := exportsOf(relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, &odd))
		Expect(exports).To(ContainElement(`export ORKA_EMULATOR_1_DEVICE_PROFILE='pixel'\''8'`))
	})

	Describe("role aliases", func() {
		It("exports a full alias set for a suffixed config", func() {
			exports, dropped := exportsOf(relayEmulator("vm-emu-1", "foo-emulator-tablet", "tablet", "192.168.64.1", 15555, nil))

			Expect(dropped).To(BeEmpty())
			Expect(exports).To(ContainElements(
				"export ORKA_EMULATOR_TABLET_ADB='192.168.64.1:15555'",
				"export ORKA_EMULATOR_TABLET_ADB_HOST='192.168.64.1'",
				"export ORKA_EMULATOR_TABLET_ADB_PORT='15555'",
				"export ORKA_EMULATOR_TABLET_NAME='vm-emu-1'",
				"export ORKA_EMULATOR_TABLET_CONFIG='foo-emulator-tablet'",
			))
		})

		It("upper-cases the suffix and turns other characters into underscores", func() {
			exports, _ := exportsOf(relayEmulator("vm-emu-1", "foo-emulator-pixel-8.pro", "pixel-8.pro", "192.168.64.1", 15555, nil))
			Expect(exports).To(ContainElement("export ORKA_EMULATOR_PIXEL_8_PRO_ADB='192.168.64.1:15555'"))
		})

		It("gives the bare form no role alias", func() {
			exports, dropped := exportsOf(relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, nil))

			Expect(dropped).To(BeEmpty())
			for _, line := range exports {
				Expect(line).ToNot(HavePrefix("export ORKA_EMULATOR__"))
			}
			Expect(exports).To(HaveLen(1 + 8 + 4))
		})

		It("drops a numeric suffix, which would shadow the indexed variables", func() {
			exports, dropped := exportsOf(
				relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, nil),
				relayEmulator("vm-emu-2", "foo-emulator-2", "2", "192.168.64.1", 15557, nil),
			)

			Expect(dropped).To(HaveLen(1))
			Expect(dropped[0].Config).To(Equal("foo-emulator-2"))
			Expect(dropped[0].Reason).To(ContainSubstring("numeric suffix"))
			// ORKA_EMULATOR_2_* still exists, but only once, as the indexed set for emulator 2.
			Expect(exports).To(ContainElement("export ORKA_EMULATOR_2_CONFIG='foo-emulator-2'"))
			Expect(exportedNames(exports)).To(HaveLen(len(exports)))
		})

		It("drops a numeric suffix even when it does not collide with an emulator that exists", func() {
			exports, dropped := exportsOf(relayEmulator("vm-emu-1", "foo-emulator-7", "7", "192.168.64.1", 15555, nil))

			Expect(dropped).To(HaveLen(1))
			Expect(exports).ToNot(ContainElement(HavePrefix("export ORKA_EMULATOR_7_")))
		})

		It("drops every set involved when two suffixes sanitize to the same name", func() {
			exports, dropped := exportsOf(
				relayEmulator("vm-emu-1", "foo-emulator-pixel-8", "pixel-8", "192.168.64.1", 15555, nil),
				relayEmulator("vm-emu-2", "foo-emulator-pixel.8", "pixel.8", "192.168.64.1", 15557, nil),
			)

			Expect(dropped).To(HaveLen(2))
			Expect(dropped[0].Reason).To(ContainSubstring("foo-emulator-pixel.8"))
			Expect(dropped[1].Reason).To(ContainSubstring("foo-emulator-pixel-8"))
			Expect(exports).ToNot(ContainElement(HavePrefix("export ORKA_EMULATOR_PIXEL_8_")))
		})

		It("keeps sets that do not collide alongside ones that do", func() {
			exports, dropped := exportsOf(
				relayEmulator("vm-emu-1", "foo-emulator-a-b", "a-b", "192.168.64.1", 15555, nil),
				relayEmulator("vm-emu-2", "foo-emulator-a.b", "a.b", "192.168.64.1", 15557, nil),
				relayEmulator("vm-emu-3", "foo-emulator-tablet", "tablet", "192.168.64.1", 15559, nil),
			)

			Expect(dropped).To(HaveLen(2))
			Expect(exports).To(ContainElement("export ORKA_EMULATOR_TABLET_ADB='192.168.64.1:15559'"))
		})

		DescribeTable("never collides with the unprefixed variables, since the suffix sits between the prefix and the field",
			func(suffix string, expectedADB string) {
				exports, dropped := exportsOf(relayEmulator("vm-emu-1", "foo-emulator-"+suffix, suffix, "192.168.64.1", 15555, nil))

				Expect(dropped).To(BeEmpty())
				Expect(exports).To(ContainElement(expectedADB))
				Expect(exports).To(ContainElement("export ORKA_EMULATOR_ADB='192.168.64.1:15555'"))
			},
			Entry("a suffix named adb", "adb", "export ORKA_EMULATOR_ADB_ADB='192.168.64.1:15555'"),
			Entry("a suffix named count", "count", "export ORKA_EMULATOR_COUNT_ADB='192.168.64.1:15555'"),
			Entry("a suffix named adb-host", "adb-host", "export ORKA_EMULATOR_ADB_HOST_ADB='192.168.64.1:15555'"),
		)

		It("exports names that are each unique", func() {
			exports, _ := exportsOf(
				relayEmulator("vm-emu-1", "foo-emulator", "", "192.168.64.1", 15555, nil),
				relayEmulator("vm-emu-2", "foo-emulator-adb", "adb", "192.168.64.1", 15557, nil),
				relayEmulator("vm-emu-3", "foo-emulator-count", "count", "192.168.64.1", 15559, nil),
				relayEmulator("vm-emu-4", "foo-emulator-tablet", "tablet", "192.168.64.1", 15561, nil),
			)

			Expect(exportedNames(exports)).To(HaveLen(len(exports)))
		})
	})
})

// exportedNames returns the set of variable names in a list of export lines.
func exportedNames(exports []string) map[string]bool {
	names := map[string]bool{}
	for _, line := range exports {
		assignment, found := strings.CutPrefix(line, "export ")
		Expect(found).To(BeTrue())
		name, _, found := strings.Cut(assignment, "=")
		Expect(found).To(BeTrue())
		names[name] = true
	}

	return names
}
