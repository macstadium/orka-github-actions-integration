package runners

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	"go.uber.org/zap"
)

type VMTracker struct {
	orkaClient    orka.OrkaService
	actionsClient actions.ActionsService
	logger        *zap.SugaredLogger

	mu         sync.Mutex
	trackedVMs map[string]int
}

func NewVMTracker(orkaClient orka.OrkaService, actionsClient actions.ActionsService, logger *zap.SugaredLogger) *VMTracker {
	return &VMTracker{
		orkaClient:    orkaClient,
		actionsClient: actionsClient,
		logger:        logger.Named("vm-tracker"),
		trackedVMs:    make(map[string]int),
	}
}

func (tracker *VMTracker) Track(vmName string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.trackedVMs[vmName] = 0
	tracker.logger.Debugf("Now tracking VM %s for orphaned VM detection", vmName)
}

func (tracker *VMTracker) Untrack(vmName string) {
	tracker.logger.Debugf("Stopping tracking VM %s for orphaned VM detection", vmName)
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.trackedVMs, vmName)
}

func (tracker *VMTracker) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tracker.checkaForOrphanedVMs(ctx)
		}
	}
}

func (tracker *VMTracker) checkaForOrphanedVMs(ctx context.Context) {
	tracker.logger.Debugf("Checking for orphaned VMs")
	tracker.mu.Lock()
	vmNames := make([]string, 0, len(tracker.trackedVMs))
	for name := range tracker.trackedVMs {
		vmNames = append(vmNames, name)
	}
	tracker.mu.Unlock()

	if len(vmNames) == 0 {
		tracker.logger.Debugf("No VMs to check for orphaned VMs")
		return
	}

	for _, name := range vmNames {
		runner, err := tracker.actionsClient.GetRunner(ctx, name)
		if err != nil {
			tracker.logger.Warnf("failed to check GitHub for %s: %v", name, err)
			continue
		}

		tracker.mu.Lock()
		if runner == nil {
			tracker.trackedVMs[name]++
			strikes := tracker.trackedVMs[name]
			tracker.mu.Unlock()

			tracker.logger.Warnf("VM %s has no GitHub runner (Strike %d/2)", name, strikes)

			if strikes >= 2 {
				tracker.logger.Errorf("VM %s is orphaned. Forcing deletion.", name)
				tracker.cleanupOrphanedVM(ctx, name)
			}
		} else {
			tracker.trackedVMs[name] = 0
			tracker.mu.Unlock()
			tracker.logger.Debugf("VM %s is healthy and registered", name)
		}
	}
}

func (tracker *VMTracker) cleanupOrphanedVM(ctx context.Context, vmName string) {
	err := tracker.orkaClient.DeleteVM(ctx, vmName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		tracker.logger.Errorf("Failed to delete orphaned VM %s: %v", vmName, err)
		return
	}

	tracker.Untrack(vmName)
	tracker.logger.Infof("Successfully deleted orphaned VM %s", vmName)
}
