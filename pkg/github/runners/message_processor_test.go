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
	breakerOpen atomic.Bool
}

func (c *countingRecorder) SetEmulatorCapacityBreakerOpen(open bool) {
	c.breakerOpen.Store(open)
}

var errCapacityFailure = fmt.Errorf("1 of 3 emulator(s) paired with VM foo-abcde failed to deploy on node mini-arm-36: %w", runnerprovisioner.ErrEmulatorCapacity)

var _ = Describe("Provisioning retries", func() {
	var (
		provisioner *fakeProvisioner
		processor   *RunnerMessageProcessor
		job         jobIdentity
	)

	BeforeEach(func() {
		logging.SetupLogger("info")
		provisioner = &fakeProvisioner{}
		processor = NewRunnerMessageProcessor(context.Background(), nil, provisioner, nil, &types.RunnerScaleSet{Id: 1, Name: "foo"}, nil)
		processor.metrics = &countingRecorder{}
		processor.newProvisionBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
		job = newJobIdentity("job-1", 42)
	})

	It("returns the runner from the first attempt that succeeds, however many failed before it", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt < 12 {
				return nil, nil, errors.New("node is full")
			}
			return &orka.VMCommandExecutor{VMName: "foo-abcde"}, []string{"set -e"}, nil
		}

		executor, _, err := processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(err).ToNot(HaveOccurred())
		Expect(executor.VMName).To(Equal("foo-abcde"))
		Expect(provisioner.attempts.Load()).To(Equal(int32(12)))
	})

	It("keeps retrying until the job is canceled upstream", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt == 12 {
				processor.setUpstreamCanceledJob(job)
			}
			return nil, nil, errors.New("node is full")
		}

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(int32(12)))
	})

	It("stops retrying when the runner shuts down", func() {
		ctx, cancel := context.WithCancel(context.Background())
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt == 3 {
				cancel()
			}
			return nil, nil, errors.New("node is full")
		}

		_, _, err := processor.provisionRunnerWithRetry(ctx, job)

		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(int32(3)))
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

	It("releases the inflight count when provisioning ends", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt == 3 {
				processor.setUpstreamCanceledJob(job)
			}
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
			if attempt <= capacityBreakerThreshold {
				return nil, nil, errCapacityFailure
			}
			return &orka.VMCommandExecutor{VMName: "foo-abcde"}, nil, nil
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

	runner := &orka.VMCommandExecutor{VMName: "foo-abcde"}

	BeforeEach(func() {
		logging.SetupLogger("info")
		provisioner = &fakeProvisioner{}
		recorder = &countingRecorder{}
		processor = NewRunnerMessageProcessor(context.Background(), nil, provisioner, nil, &types.RunnerScaleSet{Id: 1, Name: "foo"}, nil)
		processor.metrics = recorder
		processor.newProvisionBackoff = func() backoff.BackOff { return &backoff.ZeroBackOff{} }
	})

	// openBreaker fails capacityBreakerThreshold attempts in a row for lack of capacity, which opens
	// the breaker, then cancels the job so that its retries end.
	openBreaker := func() {
		job := newJobIdentity("job-1", 1)
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt > capacityBreakerThreshold {
				processor.setUpstreamCanceledJob(job)
			}
			return nil, nil, errCapacityFailure
		}

		_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(recorder.breakerOpen.Load()).To(BeTrue())
	}

	provisionAsync := func(job jobIdentity) <-chan error {
		errs := make(chan error, 1)
		go func() {
			_, _, err := processor.provisionRunnerWithRetry(context.Background(), job)
			errs <- err
		}()
		return errs
	}

	// probeUntilCanceled makes the first attempt from now on block until its job is canceled, and
	// every later attempt succeed. It returns a channel closed once that first attempt has started.
	probeUntilCanceled := func() <-chan struct{} {
		probing := make(chan struct{})
		var probed atomic.Bool
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if probed.CompareAndSwap(false, true) {
				close(probing)
				<-ctx.Done()
				return nil, nil, ctx.Err()
			}
			return runner, nil, nil
		}
		return probing
	}

	admits := func(job jobIdentity) bool {
		admitted, _ := processor.capacityBreaker.admit(job)
		return admitted
	}

	It("makes the other jobs wait while one job probes, and lets them go once it succeeds", func() {
		openBreaker()

		probing := make(chan struct{})
		release := make(chan struct{})
		var probed atomic.Bool
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if probed.CompareAndSwap(false, true) {
				close(probing)
				<-release
			}
			return runner, nil, nil
		}
		probeErr := provisionAsync(newJobIdentity("job-2", 2))
		Eventually(probing).Should(BeClosed())

		attemptsBefore := provisioner.attempts.Load()
		waitingErr := provisionAsync(newJobIdentity("job-3", 3))
		Consistently(waitingErr, 200*time.Millisecond).ShouldNot(Receive())
		Expect(provisioner.attempts.Load()).To(Equal(attemptsBefore))
		Expect(processor.provisioningInflight()).To(Equal(2))

		close(release)
		Eventually(probeErr).Should(Receive(BeNil()))
		Eventually(waitingErr).Should(Receive(BeNil()))
		Expect(recorder.breakerOpen.Load()).To(BeFalse())
	})

	It("hands the turn to a waiting job when the probing job is canceled", func() {
		openBreaker()

		probe := newJobIdentity("job-2", 2)
		probing := probeUntilCanceled()
		probeErr := provisionAsync(probe)
		Eventually(probing).Should(BeClosed())

		waitingErr := provisionAsync(newJobIdentity("job-3", 3))
		Consistently(waitingErr, 200*time.Millisecond).ShouldNot(Receive())

		processor.setUpstreamCanceledJob(probe)

		var err error
		Eventually(probeErr).Should(Receive(&err))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Eventually(waitingErr).Should(Receive(BeNil()))
		Expect(recorder.breakerOpen.Load()).To(BeFalse())
	})

	It("stops a waiting job that is canceled upstream", func() {
		openBreaker()

		probe := newJobIdentity("job-2", 2)
		probing := probeUntilCanceled()
		probeErr := provisionAsync(probe)
		Eventually(probing).Should(BeClosed())

		waiting := newJobIdentity("job-3", 3)
		attemptsBefore := provisioner.attempts.Load()
		waitingErr := provisionAsync(waiting)
		Consistently(waitingErr, 200*time.Millisecond).ShouldNot(Receive())

		processor.setUpstreamCanceledJob(waiting)

		var err error
		Eventually(waitingErr).Should(Receive(&err))
		Expect(err).To(MatchError(ContainSubstring("while waiting for its turn to provision")))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(provisioner.attempts.Load()).To(Equal(attemptsBefore))

		processor.setUpstreamCanceledJob(probe)
		Eventually(probeErr).Should(Receive())
	})

	It("does not count failures other than capacity", func() {
		job := newJobIdentity("job-1", 1)
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt == 2*capacityBreakerThreshold {
				processor.setUpstreamCanceledJob(job)
			}
			return nil, nil, errors.New("vm deploy failed")
		}
		_, _, _ = processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(recorder.breakerOpen.Load()).To(BeFalse())
		Expect(admits(newJobIdentity("job-3", 3))).To(BeTrue())
		Expect(admits(newJobIdentity("job-4", 4))).To(BeTrue())
	})

	It("starts the count over after a success", func() {
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			if attempt == capacityBreakerThreshold {
				return runner, nil, nil
			}
			return nil, nil, errCapacityFailure
		}
		_, _, err := processor.provisionRunnerWithRetry(context.Background(), newJobIdentity("job-1", 1))
		Expect(err).ToNot(HaveOccurred())

		job := newJobIdentity("job-2", 2)
		failures := 0
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			failures++
			// Cancelling the job ends its retries, and its last attempt is not counted.
			if failures == capacityBreakerThreshold {
				processor.setUpstreamCanceledJob(job)
			}
			return nil, nil, errCapacityFailure
		}
		_, _, _ = processor.provisionRunnerWithRetry(context.Background(), job)

		Expect(failures).To(Equal(capacityBreakerThreshold))
		Expect(recorder.breakerOpen.Load()).To(BeFalse())
	})

	It("does not count a cancelled attempt", func() {
		job := newJobIdentity("job-1", 1)
		provisioner.provision = func(ctx context.Context, attempt int) (*orka.VMCommandExecutor, []string, error) {
			processor.setUpstreamCanceledJob(job)
			return nil, nil, errCapacityFailure
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
