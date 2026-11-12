package emulator

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	"go.uber.org/zap"
)

// ConfigLister is the slice of the Orka client the resolver needs.
type ConfigLister interface {
	ListEmulatorConfigs(ctx context.Context) ([]*orka.OrkaEmulatorConfigResponseModel, error)
	ListVMConfigs(ctx context.Context) ([]*orka.OrkaVMConfigResponseModel, error)
}

// DanglingRecorder receives the count of emulator configs that will never be deployed, dangling or
// misnamed, after each successful pass.
type DanglingRecorder interface {
	SetEmulatorDanglingConfigs(count int)
}

// Resolver keeps a snapshot of the pairings for the VM configs this runner serves, refreshed on a
// ticker so a config added or removed in the cluster takes effect without a restart.
//
// There is no setting that turns emulators on, so the resolver must stay inert on clusters that
// cannot serve them. Until a list succeeds it reports no pairings. After that, a failed refresh
// keeps the last good snapshot, so a transient CLI or API error never makes the next runner come
// up without the emulators its jobs expect.
type Resolver struct {
	lister   ConfigLister
	served   []string
	recorder DanglingRecorder
	logger   *zap.SugaredLogger

	mu       sync.RWMutex
	snapshot *Resolution

	// Only touched from Refresh, which is never called concurrently with itself.
	clusterVMConfigs  []string
	lastConfigsError  string
	lastVMConfigError string
	lastPairings      string
	lastDangling      string
	lastMalformed     string
	lastUnserved      string
	lastSkipped       string
}

func NewResolver(lister ConfigLister, served []string, recorder DanglingRecorder, logger *zap.SugaredLogger) *Resolver {
	return &Resolver{
		lister:   lister,
		served:   served,
		recorder: recorder,
		logger:   logger,
	}
}

// Supported reports whether the cluster has ever answered an emulator config list. Before that the
// cluster may predate emulators entirely, so callers skip emulator work instead of failing on it.
func (r *Resolver) Supported() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.snapshot != nil
}

// Emulators returns the emulators to deploy alongside a VM from vmConfig, in deploy order.
func (r *Resolver) Emulators(vmConfig string) []EmulatorRef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.snapshot == nil {
		return nil
	}

	pairing, ok := r.snapshot.Pairings[vmConfig]
	if !ok {
		return nil
	}

	return append([]EmulatorRef(nil), pairing.Emulators...)
}

// Start refreshes the snapshot every interval until ctx is done. Call Refresh once beforehand so
// the first runner sees a resolved snapshot rather than an empty one.
func (r *Resolver) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.Refresh(ctx)
		}
	}
}

// Refresh runs one resolution pass. It returns the list error, if any, after logging it; the
// snapshot is left untouched in that case.
func (r *Resolver) Refresh(ctx context.Context) error {
	configs, err := r.lister.ListEmulatorConfigs(ctx)
	if err != nil {
		if r.snapshot == nil {
			r.warnOnce(&r.lastConfigsError, "unable to list Android emulator configs, Android emulators stay disabled until a list succeeds", err)
		} else {
			r.warnOnce(&r.lastConfigsError, "unable to list Android emulator configs, keeping the last resolved emulator pairings", err)
		}
		return err
	}
	r.lastConfigsError = ""

	// The VM config list only separates dangling configs from configs served by another runner.
	// If it fails, reuse the last good list; with none, report nothing as dangling rather than
	// warning about every other runner's configs.
	clusterKnown := true
	vmConfigs, vmErr := r.lister.ListVMConfigs(ctx)
	if vmErr != nil {
		r.warnOnce(&r.lastVMConfigError, "unable to list VM configs, dangling emulator config detection may be stale", vmErr)
		clusterKnown = r.clusterVMConfigs != nil
	} else {
		r.lastVMConfigError = ""
		r.clusterVMConfigs = vmConfigNames(vmConfigs)
	}

	resolution := ResolvePairings(r.served, r.clusterVMConfigs, emulatorConfigNames(configs))
	if !clusterKnown {
		resolution.Unserved = append(resolution.Unserved, resolution.Dangling...)
		resolution.Dangling = nil
	}

	r.mu.Lock()
	r.snapshot = &resolution
	r.mu.Unlock()

	r.logChanges(resolution)

	// Misnamed configs are known without the VM config list, so they are counted even when dangling
	// ones cannot be told apart from other runners' configs.
	if r.recorder != nil && (clusterKnown || len(resolution.Malformed) > 0) {
		r.recorder.SetEmulatorDanglingConfigs(len(resolution.Dangling) + len(resolution.Malformed))
	}

	return nil
}

// warnOnce warns once per distinct error, so a cluster that cannot serve emulators produces one
// line at startup rather than one per refresh. last is reset by the caller when the list succeeds.
func (r *Resolver) warnOnce(last *string, message string, err error) {
	if err.Error() == *last {
		r.logger.Debugf("%s: %v", message, err)
		return
	}

	*last = err.Error()
	r.logger.Warnf("%s: %v", message, err)
}

// logChanges reports each part of the resolution only when it differs from the last pass, since
// passes run every minute and are almost always identical.
func (r *Resolver) logChanges(resolution Resolution) {
	if summary := summarizePairings(r.served, resolution); summary != r.lastPairings {
		r.lastPairings = summary
		r.logger.Infof("Android emulator pairing: %s", summary)
	}

	if dangling := strings.Join(resolution.Dangling, ", "); dangling != r.lastDangling {
		r.lastDangling = dangling
		if dangling != "" {
			r.logger.Warnf("emulator config(s) match no VM config in the cluster and will never be deployed, check the names: %s. An emulator config pairs with VM config <name> when it is named <name>%s or <name>%s-<suffix>", dangling, Token, Token)
		} else {
			r.logger.Infof("no dangling emulator configs remain")
		}
	}

	if malformed := strings.Join(resolution.Malformed, ", "); malformed != r.lastMalformed {
		r.lastMalformed = malformed
		if malformed != "" {
			r.logger.Warnf("emulator config(s) contain %q but do not follow the naming convention and will never be deployed, check the names: %s. An emulator config pairs with VM config <name> when it is named <name>%s or <name>%s-<suffix>", Token, malformed, Token, Token)
		} else {
			r.logger.Infof("no misnamed emulator configs remain")
		}
	}

	if unserved := strings.Join(resolution.Unserved, ", "); unserved != r.lastUnserved {
		r.lastUnserved = unserved
		r.logger.Debugf("emulator config(s) belong to VM configs another runner serves: %s", unserved)
	}

	if skipped := strings.Join(resolution.Skipped, ", "); skipped != r.lastSkipped {
		r.lastSkipped = skipped
		if skipped != "" {
			r.logger.Warnf("VM config(s) %s contain %q, which is reserved for emulator config names, so no emulators are paired with them", skipped, Token)
		}
	}
}

func summarizePairings(served []string, resolution Resolution) string {
	parts := make([]string, 0, len(served))
	for _, vmConfig := range served {
		pairing, ok := resolution.Pairings[vmConfig]
		if !ok || len(pairing.Emulators) == 0 {
			parts = append(parts, vmConfig+" -> none")
			continue
		}

		configs := make([]string, 0, len(pairing.Emulators))
		for _, ref := range pairing.Emulators {
			configs = append(configs, ref.Config)
		}
		parts = append(parts, vmConfig+" -> "+strings.Join(configs, ", "))
	}

	return strings.Join(parts, "; ")
}

func emulatorConfigNames(configs []*orka.OrkaEmulatorConfigResponseModel) []string {
	names := make([]string, 0, len(configs))
	for _, config := range configs {
		if config != nil {
			names = append(names, config.Name)
		}
	}

	return names
}

func vmConfigNames(configs []*orka.OrkaVMConfigResponseModel) []string {
	names := make([]string, 0, len(configs))
	for _, config := range configs {
		if config != nil {
			names = append(names, config.Name)
		}
	}

	return names
}
