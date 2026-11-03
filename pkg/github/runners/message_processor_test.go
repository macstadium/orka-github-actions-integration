package runners

import (
	"context"
	"errors"
	"sync/atomic"

	backoff "github.com/cenkalti/backoff/v4"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
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

type countingRecorder struct {
	exhausted atomic.Int32
}

func (c *countingRecorder) IncRunnerProvisionExhausted() {
	c.exhausted.Add(1)
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

		Expect(processor.provisioningInflight.Load()).To(Equal(int32(0)))
		Expect(processor.provisionAttemptCancels).To(BeEmpty())
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
