package runners

import (
	"errors"
	"sync"

	runnerprovisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
)

// capacityBreakerThreshold is how many provisioning attempts in a row, across all of a scale set's
// jobs, must fail for lack of emulator capacity before the breaker opens. One job makes that many
// attempts in about 4 minutes.
const capacityBreakerThreshold = 5

// capacityBreaker bounds the VM churn of an emulator pairing that does not fit on the scale set's
// nodes. Every queued job retries its provisioning until it succeeds or GitHub cancels the job, so
// without the breaker each of them would keep deploying and deleting a VM. Once
// capacityBreakerThreshold attempts in a row have failed for lack of emulator capacity, the breaker
// opens and only one job at a time, the probe, may provision. The others wait for their turn. The
// first successful attempt closes it, and the waiting jobs go ahead.
//
// The breaker never refuses every job: capacity depends on load, so a node that is only busy for now
// must be able to recover without a restart, and the probe is also how a change to the pairing is
// noticed.
type capacityBreaker struct {
	mu       sync.Mutex
	failures int
	probe    jobIdentity
	probing  bool
	// turn is closed, and then replaced, when a waiting job may be able to provision: when the probe
	// gives up its turn or the breaker closes.
	turn chan struct{}
}

// isOpen reports whether the breaker is open. The caller holds mu.
func (b *capacityBreaker) isOpen() bool {
	return b.failures >= capacityBreakerThreshold
}

// admit reports whether job may start a provisioning attempt. While the breaker is open, the first
// job to ask becomes the probe and keeps the turn until release. A job that may not start also gets
// a channel that is closed when it is worth asking again.
func (b *capacityBreaker) admit(job jobIdentity) (bool, <-chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.isOpen() {
		return true, nil
	}

	if !b.probing {
		b.probe = job
		b.probing = true
		return true, nil
	}

	if b.probe == job {
		return true, nil
	}

	if b.turn == nil {
		b.turn = make(chan struct{})
	}

	return false, b.turn
}

// record counts the outcome of a provisioning attempt that was not cancelled, and reports whether it
// opened or closed the breaker. A failure for any reason other than emulator capacity says nothing
// about whether the pairing fits, so it leaves the count alone.
func (b *capacityBreaker) record(err error) (opened, closed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	wasOpen := b.isOpen()

	switch {
	case err == nil:
		b.failures = 0
		b.probing = false
		b.wakeWaiting()
	case errors.Is(err, runnerprovisioner.ErrEmulatorCapacity):
		b.failures++
	default:
		return false, false
	}

	return !wasOpen && b.isOpen(), wasOpen && !b.isOpen()
}

// release ends job's turn as the probe, if it had it, so a waiting job can take over.
func (b *capacityBreaker) release(job jobIdentity) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.probing && b.probe == job {
		b.probing = false
		b.wakeWaiting()
	}
}

// wakeWaiting wakes the jobs waiting for their turn. The caller holds mu.
func (b *capacityBreaker) wakeWaiting() {
	if b.turn != nil {
		close(b.turn)
		b.turn = nil
	}
}
