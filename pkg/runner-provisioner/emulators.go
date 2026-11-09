package provisioner

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/emulator"
	"github.com/macstadium/orka-github-actions-integration/pkg/metrics"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
)

// ErrEmulatorCapacity marks an emulator deploy that failed because the VM's node had no room for
// another emulator, as opposed to a misconfigured or broken cluster.
var ErrEmulatorCapacity = errors.New("insufficient Android emulator capacity")

// provisionedEmulator pairs a deployed emulator with the config that produced it. The deploy
// response resolves platform, image type, and device profile, but does not echo back which config
// was asked for, so it is carried alongside.
type provisionedEmulator struct {
	ref      emulator.EmulatorRef
	emulator *orka.OrkaEmulatorResponseModel
}

// emulatorName is the deterministic name of the nth emulator (1-indexed) paired with a VM, so a
// failed deploy leaves a record we can recognize and names stay stable for the job's logs.
func emulatorName(vmName string, index int) string {
	return fmt.Sprintf("%s-emu-%d", vmName, index+1)
}

func (p *RunnerProvisioner) emulatorRefs() []emulator.EmulatorRef {
	if p.emulators == nil {
		return nil
	}

	return p.emulators.Emulators(p.envData.OrkaVMConfig)
}

// deployEmulators brings up every emulator paired with the VM's config, concurrently, and fails if
// any one of them fails: a job written against N emulators cannot run against fewer. The first
// failure stops the deploys still running, so a slow one, such as a cold system image pull, does not
// hold the VM until the deploy timeout. The caller cleans up whatever did come up. Concurrency is
// safe because the operator's port allocator holds a pending reservation for each console port until
// the pod is listable; deploying in sequence would instead cost N times the deploy latency.
func (p *RunnerProvisioner) deployEmulators(ctx context.Context, vmName, node string) ([]provisionedEmulator, error) {
	refs := p.emulatorRefs()
	if len(refs) == 0 {
		return nil, nil
	}

	configs := make([]string, 0, len(refs))
	for _, ref := range refs {
		configs = append(configs, ref.Config)
	}

	p.logger.Infof("deploying %d Android emulator(s) for VM %s: %s", len(refs), vmName, strings.Join(configs, ", "))

	burstCtx, stopBurst := context.WithCancel(ctx)
	defer stopBurst()

	var wg sync.WaitGroup
	provisioned := make([]provisionedEmulator, len(refs))
	failures := make([]error, len(refs))
	stopped := make([]bool, len(refs))

	for i, ref := range refs {
		wg.Add(1)

		go func(index int, ref emulator.EmulatorRef) {
			defer wg.Done()

			err := p.deployEmulator(burstCtx, vmName, emulatorName(vmName, index), ref, &provisioned[index])
			if err == nil {
				return
			}

			// A deploy stopped because a sibling failed is not a failure of its own. Reporting it as
			// one would quote the cancellation in place of the real cause and make the burst look
			// cancelled to errors.Is. A cancel from ctx itself is still reported, since it is real.
			if errors.Is(err, context.Canceled) && ctx.Err() == nil {
				stopped[index] = true
				return
			}

			failures[index] = err
			stopBurst()
		}(i, ref)
	}

	wg.Wait()

	if err := newEmulatorDeployError(vmName, node, len(refs), failures, stopped); err != nil {
		for _, failure := range err.failures {
			p.logger.Debugf("emulator deploy failed for VM %s: %v", vmName, failure)
		}

		return nil, err
	}

	for _, entry := range provisioned {
		p.logger.Infof("emulator %s ready for VM %s at %s (config %s, platform %s, image type %s)", entry.emulator.Name, vmName, entry.emulator.ADBTarget(), entry.ref.Config, entry.emulator.Platform, entry.emulator.ImageType)
	}

	return provisioned, nil
}

func (p *RunnerProvisioner) deployEmulator(ctx context.Context, vmName, name string, ref emulator.EmulatorRef, out *provisionedEmulator) error {
	start := time.Now()

	deployed, err := p.orkaClient.DeployEmulator(ctx, name, vmName, ref.Config)
	if err != nil {
		// A cancelled deploy says nothing about the cluster, so it is not counted either way. The
		// error decides it rather than ctx alone, so a failure that lands just as a sibling stops the
		// burst is still classified.
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return fmt.Errorf("emulator %s (%s): %w", name, ref.Config, err)
		}

		result := classifyDeployError(err)
		p.metrics.RecordEmulatorDeploy(result, time.Since(start))

		if result == metrics.EmulatorDeployCapacity {
			return fmt.Errorf("emulator %s (%s): %w: %v", name, ref.Config, ErrEmulatorCapacity, err)
		}

		return fmt.Errorf("emulator %s (%s): %w", name, ref.Config, err)
	}

	// A running emulator without a relay address is unusable: the relay is the only way a job
	// inside the VM can reach it, so treat this as a provisioning failure rather than exporting an
	// empty address the workflow cannot act on.
	if !deployed.HasRelay() {
		p.metrics.RecordEmulatorDeploy(metrics.EmulatorDeployError, time.Since(start))
		return fmt.Errorf("emulator %s (%s) reported status %s with no relay address", name, ref.Config, deployed.Status)
	}

	p.metrics.RecordEmulatorDeploy(metrics.EmulatorDeploySuccess, time.Since(start))
	*out = provisionedEmulator{ref: ref, emulator: deployed}

	return nil
}

// emulatorDeployError reports a failed emulator burst on one line. A full node fails every emulator
// with near-identical CLI output, so printing each failure would turn one attempt into dozens of long
// log lines. The individual failures stay reachable through Unwrap, so errors.Is still finds
// ErrEmulatorCapacity or a cancellation, and are logged at debug level.
type emulatorDeployError struct {
	vmName   string
	node     string
	total    int
	failures []error
	// stopped counts deploys cut short by another deploy's failure.
	stopped int
}

// newEmulatorDeployError collects the non-nil failures from a burst of total deploys, or returns nil
// if every deploy succeeded.
func newEmulatorDeployError(vmName, node string, total int, results []error, stopped []bool) *emulatorDeployError {
	var failures []error
	for _, err := range results {
		if err != nil {
			failures = append(failures, err)
		}
	}

	if len(failures) == 0 {
		return nil
	}

	stoppedCount := 0
	for _, s := range stopped {
		if s {
			stoppedCount++
		}
	}

	return &emulatorDeployError{vmName: vmName, node: node, total: total, failures: failures, stopped: stoppedCount}
}

// Error summarizes the burst and quotes one failure in full, preferring one that is not a capacity
// failure, since that is the one a person needs to look at.
func (e *emulatorDeployError) Error() string {
	capacity := 0
	var example error

	for _, failure := range e.failures {
		if errors.Is(failure, ErrEmulatorCapacity) {
			capacity++
			continue
		}

		if example == nil {
			example = failure
		}
	}

	if example == nil {
		example = e.failures[0]
	}

	var counts []string
	if capacity > 0 {
		counts = append(counts, fmt.Sprintf("%d had no room on the node", capacity))
	}
	if other := len(e.failures) - capacity; other > 0 {
		counts = append(counts, fmt.Sprintf("%d failed for other reasons", other))
	}
	if e.stopped > 0 {
		counts = append(counts, fmt.Sprintf("%d still deploying were stopped", e.stopped))
	}

	return fmt.Sprintf("%d of %d emulator(s) paired with VM %s failed to deploy on node %s (%s): %v",
		len(e.failures), e.total, e.vmName, e.node, strings.Join(counts, ", "), example)
}

func (e *emulatorDeployError) Unwrap() []error {
	return e.failures
}

// classifyDeployError sorts a failed emulator deploy into a metric result. Two failures mean the
// node is full:
//
//   - The kube-scheduler refusing the pod. Its PodScheduled message names the exhausted resource,
//     for example "Insufficient orka.macstadium.com/emulator", and the operator copies it into the
//     emulator's error once AVD_UNSCHEDULABLE_TIMEOUT passes. On a default cluster the resource is
//     sized never to bind, so this only appears where the pod quota has been lowered.
//   - orka-engine timing out while creating the AVD. This is what a node at its real emulator
//     ceiling returns, within about 30 seconds of the deploy call.
func classifyDeployError(err error) string {
	message := err.Error()

	switch {
	case strings.Contains(message, "Insufficient"):
		return metrics.EmulatorDeployCapacity
	case strings.Contains(message, "failed to create AVD") && strings.Contains(message, "DeadlineExceeded"):
		return metrics.EmulatorDeployCapacity
	default:
		return metrics.EmulatorDeployError
	}
}

// deleteEmulators removes the emulators paired with a VM ahead of the VM itself. It lists them
// rather than deriving names, because the set to delete is whatever exists now: the pairing may have
// changed since deploy, a deploy may have been interrupted, and after a restart nothing in memory
// remembers what was deployed. The operator garbage-collects emulators through the VM's owner
// reference anyway, so this is best effort and must never stop the VM from being deleted.
func (p *RunnerProvisioner) deleteEmulators(ctx context.Context, vmName string) {
	emulators, err := p.orkaClient.ListEmulators(ctx)
	if err != nil {
		// On a cluster that has never served emulators the list is expected to fail on every job.
		if p.emulators != nil && p.emulators.Supported() {
			p.logger.Warnf("unable to list emulators for VM %s, falling back to owner-reference cleanup when the VM is deleted: %v", vmName, err)
		} else {
			p.logger.Debugf("unable to list emulators for VM %s: %v", vmName, err)
		}

		return
	}

	paired := orka.EmulatorsForVM(emulators, vmName)
	if len(paired) == 0 {
		return
	}

	names := make([]string, 0, len(paired))
	for _, entry := range paired {
		names = append(names, entry.Name)
	}

	p.logger.Infof("deleting emulator(s) %s for VM %s", strings.Join(names, ", "), vmName)

	if err := p.orkaClient.DeleteEmulator(ctx, names...); err != nil {
		p.logger.Warnf("failed to delete emulator(s) for VM %s, falling back to owner-reference cleanup when the VM is deleted: %v", vmName, err)
		return
	}

	p.logger.Infof("deleted emulator(s) for VM %s", vmName)
}

// roleFields are the variables exported per emulator under its config suffix, for workflows that
// address emulators by role, as in ORKA_EMULATOR_TABLET_ADB, rather than by position.
var roleFields = []string{"ADB", "ADB_HOST", "ADB_PORT", "NAME", "CONFIG"}

var nonAlphanumeric = regexp.MustCompile(`[^A-Za-z0-9]`)
var allDigits = regexp.MustCompile(`^[0-9]+$`)

// sanitizeSuffix turns a config suffix into the part of a variable name it becomes: pixel-8 becomes
// PIXEL_8.
func sanitizeSuffix(suffix string) string {
	return nonAlphanumeric.ReplaceAllString(strings.ToUpper(suffix), "_")
}

// droppedAlias is a role alias set left out of the job environment because its names collide.
type droppedAlias struct {
	Config    string
	Sanitized string
	Reason    string
}

type exportVar struct {
	name  string
	value string
}

// buildEmulatorExports renders the emulator environment as export statements, and reports any role
// alias sets it left out. Every value is single-quoted: the values come from the cluster, not from
// us, and must not be able to break out of the command stream.
//
// The variables are, in order: ORKA_EMULATOR_COUNT; ORKA_EMULATOR_<n>_* for each emulator in deploy
// order; unprefixed ORKA_EMULATOR_ADB* aliases for the first emulator; and ORKA_EMULATOR_<SUFFIX>_*
// role aliases for each suffixed config. A role alias set is exported only when none of its names
// collide with another variable. When they do, every set involved is dropped, not just the later
// one, so which emulator a name points at never depends on config ordering. A purely numeric suffix
// is always dropped, because ORKA_EMULATOR_<n>_* is reserved for the indexed variables.
func buildEmulatorExports(emulators []provisionedEmulator) ([]string, []droppedAlias) {
	if len(emulators) == 0 {
		return nil, nil
	}

	vars := []exportVar{{"ORKA_EMULATOR_COUNT", strconv.Itoa(len(emulators))}}
	targets := make([]string, 0, len(emulators))

	for i, entry := range emulators {
		deployed := entry.emulator
		prefix := fmt.Sprintf("ORKA_EMULATOR_%d", i+1)

		// deployEmulators rejects any emulator without a relay, so these are safe to read.
		targets = append(targets, deployed.ADBTarget())

		vars = append(vars,
			exportVar{prefix + "_NAME", deployed.Name},
			exportVar{prefix + "_CONFIG", entry.ref.Config},
			exportVar{prefix + "_ADB_HOST", *deployed.RelayIP},
			exportVar{prefix + "_ADB_PORT", strconv.Itoa(*deployed.RelayPort)},
			exportVar{prefix + "_ADB", deployed.ADBTarget()},
			exportVar{prefix + "_PLATFORM", deployed.Platform},
			exportVar{prefix + "_IMAGE_TYPE", deployed.ImageType},
			exportVar{prefix + "_DEVICE_PROFILE", derefOrEmpty(deployed.DeviceProfile)},
		)
	}

	// Unprefixed aliases for the common single-emulator case, so a workflow that only ever uses one
	// device does not have to carry an index.
	first := emulators[0].emulator
	vars = append(vars,
		exportVar{"ORKA_EMULATOR_ADB_HOST", *first.RelayIP},
		exportVar{"ORKA_EMULATOR_ADB_PORT", strconv.Itoa(*first.RelayPort)},
		exportVar{"ORKA_EMULATOR_ADB", targets[0]},
		exportVar{"ORKA_EMULATOR_ADB_TARGETS", strings.Join(targets, ",")},
	)

	roles, dropped := roleAliases(emulators, vars)
	vars = append(vars, roles...)

	exports := make([]string, 0, len(vars))
	for _, v := range vars {
		exports = append(exports, shellExport(v.name, v.value))
	}

	return exports, dropped
}

// roleAliases builds the role alias sets for suffixed emulators and applies the collision rule
// against taken, the indexed and unprefixed variables.
func roleAliases(emulators []provisionedEmulator, taken []exportVar) ([]exportVar, []droppedAlias) {
	reserved := make(map[string]bool, len(taken))
	for _, v := range taken {
		reserved[v.name] = true
	}

	type candidate struct {
		entry     provisionedEmulator
		sanitized string
		vars      []exportVar
	}

	var candidates []candidate
	owners := map[string][]string{}

	for _, entry := range emulators {
		if entry.ref.Suffix == "" {
			continue
		}

		sanitized := sanitizeSuffix(entry.ref.Suffix)
		prefix := "ORKA_EMULATOR_" + sanitized
		values := map[string]string{
			"ADB":      entry.emulator.ADBTarget(),
			"ADB_HOST": *entry.emulator.RelayIP,
			"ADB_PORT": strconv.Itoa(*entry.emulator.RelayPort),
			"NAME":     entry.emulator.Name,
			"CONFIG":   entry.ref.Config,
		}

		c := candidate{entry: entry, sanitized: sanitized}
		for _, field := range roleFields {
			name := prefix + "_" + field
			c.vars = append(c.vars, exportVar{name, values[field]})
			owners[name] = append(owners[name], entry.ref.Config)
		}

		candidates = append(candidates, c)
	}

	var roles []exportVar
	var dropped []droppedAlias

	for _, c := range candidates {
		var reasons []string

		if allDigits.MatchString(c.sanitized) {
			reasons = append(reasons, "a numeric suffix is reserved for the indexed ORKA_EMULATOR_<n>_* variables")
		}

		others := map[string]bool{}
		clashesWithBuiltIn := false
		for _, v := range c.vars {
			if reserved[v.name] {
				clashesWithBuiltIn = true
			}
			for _, owner := range owners[v.name] {
				if owner != c.entry.ref.Config {
					others[owner] = true
				}
			}
		}

		if clashesWithBuiltIn && len(reasons) == 0 {
			reasons = append(reasons, "its names collide with the indexed or unprefixed variables")
		}

		if len(others) > 0 {
			names := make([]string, 0, len(others))
			for owner := range others {
				names = append(names, owner)
			}
			sort.Strings(names)
			reasons = append(reasons, fmt.Sprintf("its names collide with those of emulator config(s) %s", strings.Join(names, ", ")))
		}

		if len(reasons) > 0 {
			dropped = append(dropped, droppedAlias{Config: c.entry.ref.Config, Sanitized: c.sanitized, Reason: strings.Join(reasons, "; ")})
			continue
		}

		roles = append(roles, c.vars...)
	}

	return roles, dropped
}

func shellExport(name, value string) string {
	return fmt.Sprintf("export %s=%s", name, singleQuote(value))
}

// singleQuote wraps a value for safe use in a POSIX shell, closing and reopening the quote around
// any embedded single quote.
func singleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func derefOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
