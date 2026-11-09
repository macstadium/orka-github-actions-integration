package runners

import (
	"errors"
	"sync"

	runnerprovisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
)

// capacityBreakerThreshold is how many provisioning attempts in a row, across all of a scale set's
// jobs, must fail for lack of emulator capacity before the breaker opens: as many as one job's full
// round.
const capacityBreakerThreshold = maxProvisionAttempts

// errProvisioningPaused is returned for a job that is not provisioned because the capacity breaker is
// open and another job is already probing.
var errProvisioningPaused = errors.New("provisioning is paused while another job probes for emulator capacity")

// capacityBreaker bounds the VM churn of an emulator pairing that does not fit on the scale set's
// nodes. Each job's retries are capped, but GitHub reissues an unclaimed job under a new ID, which
// starts a fresh round, and every queued job runs its own round at the same time. Once
// capacityBreakerThreshold attempts in a row have failed for lack of emulator capacity, the breaker
// opens and only one job at a time, the probe, may provision. The others give up until GitHub
// reissues them. The first successful attempt closes it.
//
// Attempts are counted rather than exhausted rounds, because the reissue usually cancels a round
// before its last attempt. And the breaker never refuses every job: capacity depends on load, so a
// node that is only busy for now must be able to recover without a restart, and the probe is also
// how a change to the pairing is noticed.
type capacityBreaker struct {
	mu       sync.Mutex
	failures int
	probe    jobIdentity
	probing  bool
}

// isOpen reports whether the breaker is open. The caller holds mu.
func (b *capacityBreaker) isOpen() bool {
	return b.failures >= capacityBreakerThreshold
}

// admit reports whether job may start a provisioning attempt. While the breaker is open, the first
// job to ask becomes the probe and keeps the turn until release.
func (b *capacityBreaker) admit(job jobIdentity) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.isOpen() {
		return true
	}

	if !b.probing {
		b.probe = job
		b.probing = true
		return true
	}

	return b.probe == job
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
	case errors.Is(err, runnerprovisioner.ErrEmulatorCapacity):
		b.failures++
	default:
		return false, false
	}

	return !wasOpen && b.isOpen(), wasOpen && !b.isOpen()
}

// release ends job's turn as the probe, if it had it, so the next job to ask can take over.
func (b *capacityBreaker) release(job jobIdentity) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.probing && b.probe == job {
		b.probing = false
	}
}
