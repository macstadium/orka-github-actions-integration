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

func (m *MockActionsService) RefreshMessageSession(ctx context.Context, runnerScaleSetId int, sessionId *uuid.UUID) (*types.RunnerScaleSetSession, error) {
	return nil, nil
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

		Context("when context is cancelled", func() {
			It("should stop polling and return", func() {
				cancelCtx, cancel := context.WithCancel(ctx)

				// Runner never de-registers, but we'll cancel the context
				mockActions.GetRunnerFunc = func(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
					// Cancel context after first check
					cancel()
					return &types.RunnerReference{Id: 888, Name: runnerName}, nil
				}
				mockActions.DeleteRunnerFunc = func(ctx context.Context, runnerID int) error {
					return nil
				}

				_ = provisioner.ensureRunnerDeregistered(cancelCtx, testRunnerName)

				// Should have attempted at least one check and force-deleted
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
