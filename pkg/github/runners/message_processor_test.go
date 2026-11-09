package runners

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	runnerprovisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeProvisioner struct {
	attempts  atomic.Int32
	provision func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error)
}

func (f *fakeProvisioner) ProvisionRunner(ctx context.Context) (*orka.VMCommandExecutor, []string, error) {
	attempt := int(f.attempts.Add(1))
	return f.provision(ctx, attempt)
}

func (f *fakeProvisioner) CleanupResources(ctx context.Context, runnerName string) {}

// signalingBackOff waits a fixed time between attempts and reports each wait as it starts.
type signalingBackOff struct {
	wait    time.Duration
	waiting chan struct{}
}

func (b *signalingBackOff) NextBackOff() time.Duration {
	select {
	case b.waiting <- struct{}{}:
	default:
	}
	return b.wait
}

func (b *signalingBackOff) Reset() {}

type fakeRunnerManager struct{}

func (fakeRunnerManager) ProcessMessages(ctx context.Context, handler func(msg *types.RunnerScaleSetMessage) error) error {
	return nil
}

func (fakeRunnerManager) AcquireJobs(ctx context.Context, requestIds []int64) error {
	return nil
}

// jobBatch builds a job message batch as GitHub delivers it, with the given assigned job count.
func jobBatch(assigned int, messages ...any) *types.RunnerScaleSetMessage {
	body, err := json.Marshal(messages)
	Expect(err).ToNot(HaveOccurred())

	return &types.RunnerScaleSetMessage{
		MessageId:   7,
		MessageType: runnerScaleSetJobMessagesType,
		Body:        string(body),
		Statistics:  &types.RunnerScaleSetStatistic{TotalAssignedJobs: assigned},
	}
}

func jobBase(messageType, jobId string, runnerRequestId int64) types.JobMessageBase {
	return types.JobMessageBase{JobMessageType: types.JobMessageType{MessageType: messageType}, JobId: jobId, RunnerRequestId: runnerRequestId}
}

type countingRecorder struct {
	exhausted   atomic.Int32
	breakerOpen atomic.Bool
}

func (c *countingRecorder) IncRunnerProvisionExhausted() {
	c.exhausted.Add(1)
}

func (c *countingRecorder) SetEmulatorCapacityBreakerOpen(open bool) {
	c.breakerOpen.Store(open)
}

var _ = Describe("Provisioning retries", func() {
	var (
		provisioner *fakeProvisioner
		recorder    *countingRecorder
		processor   *RunnerMessageProcessor
		job         jobIdentity
	)

	BeforeEach(func() {
		logging.SetupLogger("info")
		provisioner = &fakeProvisioner{}
		recorder = &countingRecorder{}
		processor = NewRunnerMessageProcessor(context.Background(), nil, provisioner, nil, &types.RunnerScaleSet{Id: 1, Name: "foo"}, nil)
		processor.metrics = recorder
		processor.newProvisionBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
		job = newJobIdentity("job-1", 42)
	})

	It("gives up after the attempt limit and counts the job as exhausted", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return nil, nil, errors.New("node is full")
		}

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(err).To(MatchError(ContainSubstring("after 5 attempts")))
		Expect(err).To(MatchError(ContainSubstring("node is full")))
		Expect(errors.Is(err, context.Canceled)).To(BeFalse())
		Expect(provisioner.attempts.Load()).To(Equal(int32(maxProvisionAttempts)))
		Expect(recorder.exhausted.Load()).To(Equal(int32(1)))
	})

	It("returns the runner from the first attempt that succeeds", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt < 3 {
				return nil, nil, errors.New("transient")
			}
			return &orka.VMCommandExecutor{VMName: "foo-abcde"}, []string{"set -e"}, nil
		}

		executor, _, err := processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(err).ToNot(HaveOccurred())
		Expect(executor.VMName).To(Equal("foo-abcde"))
		Expect(provisioner.attempts.Load()).To(Equal(int32(3)))
		Expect(recorder.exhausted.Load()).To(Equal(int32(0)))
	})

	It("interrupts an in-flight attempt when the job is canceled upstream", func() {
		started := make(chan struct{})
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			close(started)
			<-ctx.Done()
			return nil, nil, ctx.Err()
		}

		go func() {
			<-started
			processor.setUpstreamCanceledJob(job)
		}()

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(int32(1)))
		Expect(recorder.exhausted.Load()).To(Equal(int32(0)))
	})

	It("stops waiting to retry when the job is canceled upstream", func() {
		waiting := make(chan struct{}, 1)
		processor.newProvisionBackoff = func() backoff.BackOff {
			return &signalingBackOff{wait: time.Hour, waiting: waiting}
		}
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return nil, nil, errors.New("node is full")
		}
		go func() {
			<-waiting
			processor.setUpstreamCanceledJob(job)
		}()

		errs := make(chan error, 1)
		go func() {
			_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)
			errs <- err
		}()

		var err error
		Eventually(errs, 5*time.Second).Should(Receive(&err))
		Expect(err).To(MatchError(ContainSubstring("canceled upstream while waiting to retry")))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(int32(1)))
		Expect(processor.provisionCancels).To(BeEmpty())
	})

	It("only interrupts the attempt for the job that was canceled", func() {
		other := newJobIdentity("job-2", 43)
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			processor.setUpstreamCanceledJob(other)
			Expect(ctx.Err()).ToNot(HaveOccurred())
			return &orka.VMCommandExecutor{VMName: "foo-abcde"}, nil, nil
		}

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)
		Expect(err).ToNot(HaveOccurred())
	})

	It("does not start an attempt for a job already canceled upstream", func() {
		processor.setUpstreamCanceledJob(job)

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(int32(0)))
	})

	It("releases the inflight count when provisioning is exhausted", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return nil, nil, errors.New("node is full")
		}

		processor.startRunner(job)

		Expect(processor.provisioningInflight()).To(Equal(0))
		Expect(processor.provisionCancels).To(BeEmpty())
		Expect(processor.upstreamCanceledJobs).To(BeEmpty())
	})

	It("uses a nil metrics handle without panicking", func() {
		processor = NewRunnerMessageProcessor(context.Background(), nil, provisioner, nil, &types.RunnerScaleSet{Id: 1, Name: "foo"}, nil)
		processor.newProvisionBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return nil, nil, errors.New("node is full")
		}

		Expect(func() { _, _, _ = processor.provisionRunnerWithRetry(context.Background(), job) }).ToNot(Panic())
	})
})

var _ = Describe("The emulator capacity breaker", func() {
	var (
		provisioner *fakeProvisioner
		recorder    *countingRecorder
		processor   *RunnerMessageProcessor
	)

	capacityFailure := fmt.Errorf("1 of 3 emulator(s) paired with VM foo-abcde failed to deploy on node mini-arm-36: %w", runnerprovisioner.ErrEmulatorCapacity)
	runner := &orka.VMCommandExecutor{VMName: "foo-abcde"}

	BeforeEach(func() {
		logging.SetupLogger("info")
		provisioner = &fakeProvisioner{}
		recorder = &countingRecorder{}
		processor = NewRunnerMessageProcessor(context.Background(), nil, provisioner, nil, &types.RunnerScaleSet{Id: 1, Name: "foo"}, nil)
		processor.metrics = recorder
		processor.newProvisionBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
	})

	// failRound runs one job through a full round of capacity failures, which opens the breaker.
	failRound := func(job jobIdentity) {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return nil, nil, capacityFailure
		}

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)
		Expect(errors.Is(err, runnerprovisioner.ErrEmulatorCapacity)).To(BeTrue())
	}

	It("lets one job at a time provision once capacity failures pile up, until one succeeds", func() {
		failRound(newJobIdentity("job-1", 1))
		Expect(recorder.breakerOpen.Load()).To(BeTrue())

		probing := make(chan struct{})
		release := make(chan struct{})
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			close(probing)
			<-release
			return runner, nil, nil
		}
		probeErr := make(chan error, 1)
		go func() {
			_, _, err := processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-2", 2))
			probeErr <- err
		}()
		<-probing

		attemptsBefore := provisioner.attempts.Load()
		_, _, err := processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-3", 3))
		Expect(errors.Is(err, errProvisioningPaused)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(attemptsBefore))

		close(release)
		Eventually(probeErr).Should(Receive(BeNil()))
		Expect(recorder.breakerOpen.Load()).To(BeFalse())

		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return runner, nil, nil
		}
		_, _, err = processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-3", 3))
		Expect(err).ToNot(HaveOccurred())
	})

	It("hands the probe to the next job once the probing job's round ends without a success", func() {
		failRound(newJobIdentity("job-1", 1))

		probing := make(chan struct{}, 1)
		release := make(chan struct{})
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			select {
			case probing <- struct{}{}:
			default:
			}
			<-release
			return nil, nil, capacityFailure
		}
		probeDone := make(chan struct{})
		go func() {
			defer close(probeDone)
			_, _, _ = processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-2", 2))
		}()
		Eventually(probing).Should(Receive())

		Expect(processor.capacityBreaker.admit(newJobIdentity("job-3", 3))).To(BeFalse())

		close(release)
		Eventually(probeDone).Should(BeClosed())

		Expect(recorder.breakerOpen.Load()).To(BeTrue())
		Expect(processor.capacityBreaker.admit(newJobIdentity("job-3", 3))).To(BeTrue())
	})

	It("does not count failures other than capacity", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			return nil, nil, errors.New("vm deploy failed")
		}
		_, _, _ = processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-1", 1))
		_, _, _ = processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-2", 2))

		Expect(recorder.breakerOpen.Load()).To(BeFalse())
		Expect(processor.capacityBreaker.admit(newJobIdentity("job-3", 3))).To(BeTrue())
		Expect(processor.capacityBreaker.admit(newJobIdentity("job-4", 4))).To(BeTrue())
	})

	It("starts the count over after a success", func() {
		job := newJobIdentity("job-1", 1)
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt == capacityBreakerThreshold {
				return runner, nil, nil
			}
			return nil, nil, capacityFailure
		}
		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)
		Expect(err).ToNot(HaveOccurred())

		failures := 0
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			failures++
			return nil, nil, capacityFailure
		}
		processor.newProvisionBackoff = func() backoff.BackOff {
			return backoff.WithMaxRetries(&backoff.ZeroBackOff{}, capacityBreakerThreshold-2)
		}
		_, _, _ = processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-2", 2))

		Expect(failures).To(Equal(capacityBreakerThreshold - 1))
		Expect(recorder.breakerOpen.Load()).To(BeFalse())
	})

	It("does not count a cancelled attempt", func() {
		job := newJobIdentity("job-1", 1)
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			processor.setUpstreamCanceledJob(job)
			return nil, nil, capacityFailure
		}
		for range capacityBreakerThreshold {
			_, _, _ = processor.provisionRunnerWithRetry(context.Background(), job)
			processor.removeUpstreamCanceledJob(job)
		}

		Expect(recorder.breakerOpen.Load()).To(BeFalse())
	})
})

var _ = Describe("Scaling for assigned jobs", func() {
	var (
		provisioner *fakeProvisioner
		processor   *RunnerMessageProcessor
		started     chan int
	)

	BeforeEach(func() {
		logging.SetupLogger("info")
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		started = make(chan int, 4)
		provisioner = &fakeProvisioner{
			provision: func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
				started <- attempt
				<-ctx.Done()
				return nil, nil, ctx.Err()
			},
		}
		processor = NewRunnerMessageProcessor(ctx, fakeRunnerManager{}, provisioner, nil, &types.RunnerScaleSet{Id: 1, Name: "foo"}, nil)
		processor.newProvisionBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
	})

	It("provisions a reissued job that arrives in the same batch as its original's cancellation", func() {
		go processor.startRunner(newJobIdentity("job-1", 42))
		Eventually(started).Should(Receive(Equal(1)))
		Expect(processor.provisioningInflight()).To(Equal(1))

		Expect(processor.processRunnerMessage(jobBatch(1,
			types.JobCompleted{Result: cancelledStatus, JobMessageBase: jobBase("JobCompleted", "job-1", 42)},
			types.JobAssigned{JobMessageBase: jobBase("JobAssigned", "job-2", 43)},
		))).To(Succeed())

		Eventually(started).Should(Receive(Equal(2)))
	})

	It("still counts a job in flight that was not canceled", func() {
		go processor.startRunner(newJobIdentity("job-1", 42))
		Eventually(started).Should(Receive(Equal(1)))

		Expect(processor.processRunnerMessage(jobBatch(1,
			types.JobAssigned{JobMessageBase: jobBase("JobAssigned", "job-1", 42)},
		))).To(Succeed())

		Consistently(started, 200*time.Millisecond).ShouldNot(Receive())
	})
})
