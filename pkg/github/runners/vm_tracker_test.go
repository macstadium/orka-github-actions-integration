package runners

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestVMTracker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VMTracker Suite")
}

type MockOrkaClient struct {
	DeleteVMFunc func(ctx context.Context, name string) error
	DeployVMFunc func(ctx context.Context, namePrefix, vmConfig string) (*orka.OrkaVMDeployResponseModel, error)
}

func (m *MockOrkaClient) DeleteVM(ctx context.Context, name string) error {
	if m.DeleteVMFunc != nil {
		return m.DeleteVMFunc(ctx, name)
	}
	return nil
}

func (m *MockOrkaClient) DeployVM(ctx context.Context, namePrefix, vmConfig string) (*orka.OrkaVMDeployResponseModel, error) {
	if m.DeployVMFunc != nil {
		return m.DeployVMFunc(ctx, namePrefix, vmConfig)
	}
	return nil, nil
}

type MockActionsClient struct {
	GetRunnerFunc func(ctx context.Context, runnerName string) (*types.RunnerReference, error)
}

func (m *MockActionsClient) GetRunner(ctx context.Context, runnerName string) (*types.RunnerReference, error) {
	if m.GetRunnerFunc != nil {
		return m.GetRunnerFunc(ctx, runnerName)
	}
	return nil, nil
}

func (m *MockActionsClient) GetRunnerScaleSet(ctx context.Context, id int, name string) (*types.RunnerScaleSet, error) {
	return nil, nil
}
func (m *MockActionsClient) CreateRunnerScaleSet(ctx context.Context, rs *types.RunnerScaleSet) (*types.RunnerScaleSet, error) {
	return nil, nil
}
func (m *MockActionsClient) DeleteRunnerScaleSet(ctx context.Context, id int) error { return nil }
func (m *MockActionsClient) CreateRunner(ctx context.Context, id int, name string) (*types.RunnerScaleSetJitRunnerConfig, error) {
	return nil, nil
}
func (m *MockActionsClient) DeleteRunner(ctx context.Context, id int) error { return nil }
func (m *MockActionsClient) CreateMessageSession(ctx context.Context, id int, owner string) (*types.RunnerScaleSetSession, error) {
	return nil, nil
}
func (m *MockActionsClient) DeleteMessageSession(ctx context.Context, id int, sessionId *uuid.UUID) error {
	return nil
}
func (m *MockActionsClient) RefreshMessageSession(ctx context.Context, id int, sessionId *uuid.UUID) (*types.RunnerScaleSetSession, error) {
	return nil, nil
}
func (m *MockActionsClient) AcquireJobs(ctx context.Context, id int, token string, reqIds []int64) ([]int64, error) {
	return nil, nil
}
func (m *MockActionsClient) GetAcquirableJobs(ctx context.Context, id int) (*types.AcquirableJobList, error) {
	return nil, nil
}
func (m *MockActionsClient) GetMessage(ctx context.Context, url, token string, lastId int64) (*types.RunnerScaleSetMessage, error) {
	return nil, nil
}
func (m *MockActionsClient) DeleteMessage(ctx context.Context, url, token string, id int64) error {
	return nil
}

var _ = Describe("VMTracker", func() {
	var (
		tracker     *VMTracker
		mockOrka    *MockOrkaClient
		mockActions *MockActionsClient
		ctx         context.Context
		vmName      string
	)

	BeforeEach(func() {
		mockOrka = &MockOrkaClient{}
		mockActions = &MockActionsClient{}
		logger := zap.NewNop().Sugar()
		ctx = context.Background()
		vmName = "orka-vm-test-1"

		tracker = NewVMTracker(mockOrka, mockActions, logger)
	})

	Describe("Tracking State", func() {
		It("should verify a VM is tracked after calling Track", func() {
			tracker.Track(vmName)

			count, exists := tracker.trackedVMs[vmName]

			Expect(exists).To(BeTrue(), "VM should exist in map")
			Expect(count).To(Equal(0), "Initial strikes should be 0")
		})

		It("should stop tracking a VM after calling Untrack", func() {
			tracker.Track(vmName)
			tracker.Untrack(vmName)

			_, exists := tracker.trackedVMs[vmName]

			Expect(exists).To(BeFalse(), "VM should be removed from map")
		})
	})

	Describe("Check Cycle", func() {

		Context("When the VM is new (Strike 0)", func() {
			BeforeEach(func() {
				tracker.Track(vmName)
			})

			It("should remain healthy if GitHub returns the runner", func() {
				mockActions.GetRunnerFunc = func(c context.Context, n string) (*types.RunnerReference, error) {
					return &types.RunnerReference{Name: vmName, Id: 123}, nil
				}

				tracker.checkaForOrphanedVMs(ctx)

				strikes := tracker.trackedVMs[vmName]
				Expect(strikes).To(Equal(0), "Strikes should remain 0 for healthy runner")
			})

			It("should apply Strike 1 if Runner is missing", func() {
				mockActions.GetRunnerFunc = func(c context.Context, n string) (*types.RunnerReference, error) {
					return nil, nil
				}

				mockOrka.DeleteVMFunc = func(c context.Context, n string) error {
					Fail("DeleteVM should not be called on first strike")
					return nil
				}

				tracker.checkaForOrphanedVMs(ctx)

				strikes := tracker.trackedVMs[vmName]
				Expect(strikes).To(Equal(1), "Strikes should increment to 1")
			})
		})

		Context("When the VM has 1 Strike", func() {
			BeforeEach(func() {
				tracker.Track(vmName)
				tracker.trackedVMs[vmName] = 1
			})

			It("should Reset strikes to 0 if Runner appears", func() {
				mockActions.GetRunnerFunc = func(c context.Context, n string) (*types.RunnerReference, error) {
					return &types.RunnerReference{Name: vmName, Id: 123}, nil
				}

				tracker.checkaForOrphanedVMs(ctx)

				strikes := tracker.trackedVMs[vmName]
				Expect(strikes).To(Equal(0), "Strikes should reset to 0 upon recovery")
			})

			It("should Delete the VM if Runner is still missing (Strike 2)", func() {
				mockActions.GetRunnerFunc = func(c context.Context, n string) (*types.RunnerReference, error) {
					return nil, nil
				}

				deleteCalled := false
				mockOrka.DeleteVMFunc = func(c context.Context, n string) error {
					Expect(n).To(Equal(vmName))
					deleteCalled = true
					return nil
				}

				tracker.checkaForOrphanedVMs(ctx)

				Expect(deleteCalled).To(BeTrue(), "DeleteVM must be called on 2nd strike")

				_, exists := tracker.trackedVMs[vmName]
				Expect(exists).To(BeFalse(), "VM should be untracked after deletion")
			})
		})

		Context("When GitHub API fails", func() {
			It("should ignore API errors and NOT apply strikes", func() {
				tracker.Track(vmName)

				mockActions.GetRunnerFunc = func(c context.Context, n string) (*types.RunnerReference, error) {
					return nil, errors.New("500 Internal Server Error")
				}

				tracker.checkaForOrphanedVMs(ctx)
				tracker.checkaForOrphanedVMs(ctx)

				strikes := tracker.trackedVMs[vmName]
				Expect(strikes).To(Equal(0), "Strikes should not increase on API errors")
			})
		})
	})
})
