// Licensed under the Apache License, Version 2.0
// Original work from the Actions Runner Controller (ARC) project
// See https://github.com/actions/actions-runner-controller

package runners

import (
	"context"
	"testing"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func init() {
	logging.SetupLogger("info")
}

type MockRunnerManager struct {
	mock.Mock
}

func (m *MockRunnerManager) ProcessMessages(ctx context.Context, handler func(msg *types.RunnerScaleSetMessage) error) error {
	args := m.Called(ctx, handler)
	return args.Error(0)
}

func (m *MockRunnerManager) AcquireJobs(ctx context.Context, requestIds []int64) error {
	args := m.Called(ctx, requestIds)
	return args.Error(0)
}

type MockRunnerProvisioner struct {
	mock.Mock
}

func (m *MockRunnerProvisioner) ProvisionRunner(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestTrackAcquiredJob(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	runnerRequestId := int64(12345)
	jobId := "job-abc-123"

	processor.trackAcquiredJob(runnerRequestId, jobId)

	assert.True(t, processor.isJobAcquired(runnerRequestId))

	jobs := processor.getAcquiredJobs()
	assert.Len(t, jobs, 1)
	assert.Equal(t, runnerRequestId, jobs[0].RunnerRequestId)
	assert.Equal(t, jobId, jobs[0].JobId)
	assert.Equal(t, 0, jobs[0].RetryCount)
}

func TestRemoveAcquiredJob(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	runnerRequestId := int64(12345)
	jobId := "job-abc-123"

	processor.trackAcquiredJob(runnerRequestId, jobId)
	assert.True(t, processor.isJobAcquired(runnerRequestId))

	processor.removeAcquiredJob(runnerRequestId)
	assert.False(t, processor.isJobAcquired(runnerRequestId))

	jobs := processor.getAcquiredJobs()
	assert.Len(t, jobs, 0)
}

func TestIsJobAcquired(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	runnerRequestId := int64(12345)
	jobId := "job-abc-123"

	assert.False(t, processor.isJobAcquired(runnerRequestId))

	processor.trackAcquiredJob(runnerRequestId, jobId)
	assert.True(t, processor.isJobAcquired(runnerRequestId))
}

func TestGetAcquiredJobs(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	jobs := processor.getAcquiredJobs()
	assert.Len(t, jobs, 0)

	processor.trackAcquiredJob(int64(1), "job-1")
	processor.trackAcquiredJob(int64(2), "job-2")
	processor.trackAcquiredJob(int64(3), "job-3")

	jobs = processor.getAcquiredJobs()
	assert.Len(t, jobs, 3)

	runnerRequestIds := make(map[int64]bool)
	for _, job := range jobs {
		runnerRequestIds[job.RunnerRequestId] = true
	}
	assert.True(t, runnerRequestIds[1])
	assert.True(t, runnerRequestIds[2])
	assert.True(t, runnerRequestIds[3])
}

func TestLogStuckJobs_NoStuckJobs(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	processor.trackAcquiredJob(int64(1), "job-1")

	processor.logStuckJobs()
}

func TestLogStuckJobs_WithStuckJob(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	runnerRequestId := int64(12345)
	processor.trackAcquiredJob(runnerRequestId, "job-abc-123")

	job := processor.acquiredJobs[runnerRequestId]
	job.AcquiredAt = time.Now().Add(-6 * time.Minute)

	processor.logStuckJobs()
}

func TestCanceledJobFunctions(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	jobId := "job-abc-123"

	assert.False(t, processor.isCanceled(jobId))

	processor.setCanceledJob(jobId)
	assert.True(t, processor.isCanceled(jobId))

	processor.removeCanceledJob(jobId)
	assert.False(t, processor.isCanceled(jobId))
}

func TestConcurrentJobTracking(t *testing.T) {
	ctx := context.Background()
	mockManager := new(MockRunnerManager)
	mockProvisioner := new(MockRunnerProvisioner)
	runnerScaleSet := &types.RunnerScaleSet{Id: 1, Name: "test-runner"}

	processor := NewRunnerMessageProcessor(ctx, mockManager, mockProvisioner, runnerScaleSet)

	done := make(chan bool)

	go func() {
		for i := range 100 {
			processor.trackAcquiredJob(int64(i), "job")
		}
		done <- true
	}()

	go func() {
		for i := range 100 {
			processor.isJobAcquired(int64(i))
		}
		done <- true
	}()

	go func() {
		for i := range 100 {
			processor.removeAcquiredJob(int64(i))
		}
		done <- true
	}()

	<-done
	<-done
	<-done
}
