package provisioner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProvisioner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provisioner Suite")
}

// MockActionsService is a mock implementation of actions.ActionsService
type MockActionsService struct {
	GetRunnerFunc     func(ctx context.Context, runnerName string) (*types.RunnerReference, error)
	DeleteRunnerFunc  func(ctx context.Context, runnerID int) error
	GetRunnerCalls    int
	DeleteRunnerCalls int
}

func (m *MockActionsService) GetRunner(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
	m.GetRunnerCalls++
	if m.GetRunnerFunc != nil {
		return m.GetRunnerFunc(ctx, runnerName)
	}
	return nil, nil
}

func (m *MockActionsService) DeleteRunner(ctx context.Context, runnerID int) error {
	m.DeleteRunnerCalls++
	if m.DeleteRunnerFunc != nil {
		return m.DeleteRunnerFunc(ctx, runnerID)
	}
	return nil
}

// Stub implementations for other ActionsService methods
func (m *MockActionsService) GetRunnerScaleSet(ctx context.Context, runnerGroupId int, runnerScaleSetName string) (*types.RunnerScaleSet, error) {
	return nil, nil
}

func (m *MockActionsService) CreateRunnerScaleSet(ctx context.Context, runnerScaleSet *types.RunnerScaleSet) (*types.RunnerScaleSet, error) {
	return nil, nil
}

func (m *MockActionsService) DeleteRunnerScaleSet(ctx context.Context, runnerScaleSetId int) error {
	return nil
}

func (m *MockActionsService) CreateRunner(ctx context.Context, runnerScaleSetID int, runnerName string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	return nil, nil
}

func (m *MockActionsService) CreateMessageSession(ctx context.Context, runnerScaleSetId int, owner string) (*types.RunnerScaleSetSession, error) {
	return nil, nil
}

func (m *MockActionsService) DeleteMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) error {
	return nil
}

func (m *MockActionsService) AcquireJobs(ctx context.Context, runnerScaleSetId int, messageQueueAccessToken string, requestIds []int64) ([]int64, error) {
	return nil, nil
}

func (m *MockActionsService) GetAcquirableJobs(ctx context.Context, runnerScaleSetId int) (*types.AcquirableJobList, error) {
	return nil, nil
}

func (m *MockActionsService) GetMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, lastMessageId int64) (*types.RunnerScaleSetMessage, error) {
	return nil, nil
}

func (m *MockActionsService) DeleteMessage(ctx context.Context, messageQueueUrl, messageQueueAccessToken string, messageId int64) error {
	return nil
}

const testRunnerName = "test-runner-1"

var _ = Describe("RunnerProvisioner", func() {
	var (
		provisioner *RunnerProvisioner
		mockActions *MockActionsService
		ctx         context.Context
	)

	BeforeEach(func() {
		logging.SetupLogger("info")
		ctx = context.Background()
		mockActions = &MockActionsService{}
		provisioner = &RunnerProvisioner{
			actionsClient: mockActions,
			runnerScaleSet: &types.RunnerScaleSet{
				Id:   1,
				Name: "test-runner",
			},
			envData: &env.Data{
				RunnerDeregistrationTimeout:      5 * time.Second,
				RunnerDeregistrationPollInterval: 100 * time.Millisecond,
			},
			logger: logging.Logger.Named("test-provisioner"),
		}
	})

	Describe("ensureRunnerDeregistered", func() {
		Context("when runner de-registers within timeout", func() {
			It("should return without force-deleting", func() {
				callCount := 0
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					callCount++
					if callCount >= 2 {
						// Runner is gone on second call
						return nil, nil
					}
					// Runner still exists on first call
					return &types.RunnerReference{Id: 123, Name: runnerName}, nil
				}

				err := provisioner.ensureRunnerDeregistered(ctx, testRunnerName)
				Expect(err).To(BeNil())

				Expect(mockActions.GetRunnerCalls).To(BeNumerically(">=", 2))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(0))
			})
		})

		Context("when runner is already de-registered", func() {
			It("should return immediately without force-deleting", func() {
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					return nil, nil // Runner not found
				}

				err := provisioner.ensureRunnerDeregistered(ctx, testRunnerName)
				Expect(err).To(BeNil())

				Expect(mockActions.GetRunnerCalls).To(Equal(1))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(0))
			})
		})

		Context("when GetRunner returns transient errors", func() {
			It("should continue polling and log warnings", func() {
				callCount := 0
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					callCount++
					if callCount == 1 {
						return nil, errors.New("transient network error")
					}
					// Runner gone on second call
					return nil, nil
				}

				err := provisioner.ensureRunnerDeregistered(ctx, testRunnerName)
				Expect(err).To(BeNil())

				Expect(mockActions.GetRunnerCalls).To(BeNumerically(">=", 2))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(0))
			})
		})

		Context("when runner does not de-register within timeout", func() {
			It("should force-delete the runner", func() {
				// Use a very short timeout for this test
				provisioner.envData.RunnerDeregistrationTimeout = 200 * time.Millisecond
				provisioner.envData.RunnerDeregistrationPollInterval = 50 * time.Millisecond

				// Runner never de-registers
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					return &types.RunnerReference{Id: 999, Name: runnerName}, nil
				}
				mockActions.DeleteRunnerFunc = func(ctx context.Context, runnerID int) error {
					Expect(runnerID).To(Equal(999))
					return nil
				}

				err := provisioner.ensureRunnerDeregistered(ctx, testRunnerName)
				Expect(err).To(BeNil())

				Expect(mockActions.GetRunnerCalls).To(BeNumerically(">=", 1))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(1))
			})
		})
	})

	Describe("forceDeleteRunner", func() {
		Context("when runner exists", func() {
			It("should delete the runner successfully", func() {
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					return &types.RunnerReference{Id: 456, Name: runnerName}, nil
				}
				mockActions.DeleteRunnerFunc = func(ctx context.Context, runnerID int) error {
					Expect(runnerID).To(Equal(456))
					return nil
				}

				err := provisioner.forceDeleteRunner(ctx, testRunnerName)
				Expect(err).To(BeNil())

				Expect(mockActions.GetRunnerCalls).To(Equal(1))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(1))
			})
		})

		Context("when runner is already gone", func() {
			It("should not attempt to delete", func() {
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					return nil, nil // Runner not found
				}

				err := provisioner.forceDeleteRunner(ctx, testRunnerName)
				Expect(err).To(BeNil())

				Expect(mockActions.GetRunnerCalls).To(Equal(1))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(0))
			})
		})

		Context("when GetRunner fails", func() {
			It("should log error and not attempt delete", func() {
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					return nil, errors.New("API error")
				}

				err := provisioner.forceDeleteRunner(ctx, testRunnerName)
				Expect(err).To(Not(BeNil()))

				Expect(mockActions.GetRunnerCalls).To(Equal(1))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(0))
			})
		})

		Context("when DeleteRunner fails", func() {
			It("should log error", func() {
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					return &types.RunnerReference{Id: 789, Name: runnerName}, nil
				}
				mockActions.DeleteRunnerFunc = func(ctx context.Context, runnerID int) error {
					return errors.New("delete failed")
				}

				err := provisioner.forceDeleteRunner(ctx, testRunnerName)
				Expect(err).To(Not(BeNil()))

				Expect(mockActions.GetRunnerCalls).To(Equal(1))
				Expect(mockActions.DeleteRunnerCalls).To(Equal(1))
			})
		})
	})
})

func relayEmulator(name, config, host string, port int, platform, imageType string, deviceProfile *string) provisionedEmulator {
	return provisionedEmulator{
		config: config,
		emulator: &orka.OrkaEmulatorResponseModel{
			Name:          name,
			Status:        orka.EmulatorRunning,
			Platform:      platform,
			ImageType:     imageType,
			DeviceProfile: deviceProfile,
			RelayIP:       &host,
			RelayPort:     &port,
		},
	}
}

var _ = Describe("Emulator naming", func() {
	DescribeTable("derives a deterministic name from the VM and list position",
		func(vmName string, index int, expected string) {
			Expect(emulatorName(vmName, index)).To(Equal(expected))
		},
		Entry("first emulator is 1-indexed", "my-runner-abc123", 0, "my-runner-abc123-emu-1"),
		Entry("second emulator follows the list order", "my-runner-abc123", 1, "my-runner-abc123-emu-2"),
		Entry("tenth emulator is not zero padded", "my-runner-abc123", 9, "my-runner-abc123-emu-10"),
	)

	It("produces names that are recomputable at cleanup time without any stored state", func() {
		vmName := "my-runner-abc123"
		configs := []string{"pixel8-api36", "tablet-api35"}

		names := make([]string, 0, len(configs))
		for i := range configs {
			names = append(names, emulatorName(vmName, i))
		}

		Expect(names).To(Equal([]string{"my-runner-abc123-emu-1", "my-runner-abc123-emu-2"}))
	})
})

var _ = Describe("Emulator exports", func() {
	It("emits nothing when no emulators were provisioned", func() {
		Expect(buildEmulatorExports(nil)).To(BeNil())
	})

	It("leaves the runner commands untouched when emulators are disabled", func() {
		commands := buildCommands("jit", "2.336.0", "admin", nil)
		Expect(commands).To(HaveLen(len(commands_template)))
		Expect(commands[0]).To(Equal("set -e"))
	})

	It("prepends exports ahead of the runner commands", func() {
		profile := "pixel_8"
		commands := buildCommands("jit", "2.336.0", "admin", []provisionedEmulator{
			relayEmulator("vm-emu-1", "pixel8-api36", "192.168.64.1", 15555, "android-36", "google_apis", &profile),
		})

		Expect(commands[0]).To(Equal("export ORKA_EMULATOR_COUNT='1'"))
		Expect(commands).To(ContainElement("set -e"))
	})

	It("exports the indexed variables and the single-emulator aliases", func() {
		profile := "pixel_8"
		exports := buildEmulatorExports([]provisionedEmulator{
			relayEmulator("vm-emu-1", "pixel8-api36", "192.168.64.1", 15555, "android-36", "google_apis", &profile),
		})

		Expect(exports).To(ContainElements(
			"export ORKA_EMULATOR_COUNT='1'",
			"export ORKA_EMULATOR_1_NAME='vm-emu-1'",
			"export ORKA_EMULATOR_1_CONFIG='pixel8-api36'",
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
		exports := buildEmulatorExports([]provisionedEmulator{
			relayEmulator("vm-emu-1", "default-api36", "192.168.64.1", 15555, "android-36", "default", nil),
		})

		Expect(exports).To(ContainElement("export ORKA_EMULATOR_1_DEVICE_PROFILE=''"))
	})

	It("points the aliases at the first emulator and lists every target", func() {
		first := "pixel_8"
		second := "pixel_tablet"
		exports := buildEmulatorExports([]provisionedEmulator{
			relayEmulator("vm-emu-1", "pixel8-api36", "192.168.64.1", 15555, "android-36", "google_apis", &first),
			relayEmulator("vm-emu-2", "tablet-api35", "192.168.64.1", 15557, "android-35", "google_apis", &second),
		})

		Expect(exports).To(ContainElements(
			"export ORKA_EMULATOR_COUNT='2'",
			"export ORKA_EMULATOR_2_ADB='192.168.64.1:15557'",
			"export ORKA_EMULATOR_2_CONFIG='tablet-api35'",
			"export ORKA_EMULATOR_ADB='192.168.64.1:15555'",
			"export ORKA_EMULATOR_ADB_TARGETS='192.168.64.1:15555,192.168.64.1:15557'",
		))
	})

	It("quotes values so a cluster-supplied string cannot break out of the command stream", func() {
		odd := "pixel'8"
		exports := buildEmulatorExports([]provisionedEmulator{
			relayEmulator("vm-emu-1", "cfg", "192.168.64.1", 15555, "android-36", "google_apis", &odd),
		})

		Expect(exports).To(ContainElement(`export ORKA_EMULATOR_1_DEVICE_PROFILE='pixel'\''8'`))
	})
})
