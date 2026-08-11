// Spike (OK-5518, Track A): N scale sets managed by one controller process, kept in sync
// with the live set of `gha-*` Orka VM configs by a continuous reconcile loop.
// Not wired into `make build`/`make run` - a separate binary, run manually against a
// real GitHub org + Orka cluster. Findings: docs/04-spike-findings.md (spike doc series).
//
// Proves: live discovery (`orka3 vmc list` via OrkaService.ListVMConfigs, filtered to the
// "gha-" prefix), reconcile-by-listing against owned GitHub scale sets, ownership-prefix
// scoping, per-set alias labels, and running one independent
// RunnerManager/RunnerProvisioner/RunnerMessageProcessor triple per scale set as a
// goroutine - started when a VM config appears and stopped when it disappears, re-checked
// every reconcileInterval.
//
// NOT implemented here (see findings for why): drain-on-delete (a removed config's scale
// set is deleted immediately, cancelling its goroutine mid-flight, same as today's single-
// set cleanup), stale-session recreate (main.go's ErrActiveSession handling - orthogonal
// to the fleet question, reuse as-is), per-set sizing threaded through the provisioner
// (still a shallow envData copy - the shared provisioner.go is left untouched on purpose).
package main

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/constants"
	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/runners"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	provisioner "github.com/macstadium/orka-github-actions-integration/pkg/runner-provisioner"
	"go.uber.org/zap"
)

// reconcileInterval is how often the fleet is re-synced against the live `gha-*` VM config
// list. Short on purpose for the spike, to watch configs appear and disappear.
const reconcileInterval = 10 * time.Second

// vmConfigPrefix opts a VM config into the GitHub Actions fleet by naming convention.
const vmConfigPrefix = "gha-"

// runningMember is one scale set the controller currently runs a goroutine for. cancel
// stops that goroutine (and, with it, message acquisition) so the scale set can be deleted.
type runningMember struct {
	scaleSet *types.RunnerScaleSet
	cancel   context.CancelFunc
}

func main() {
	envData := env.ParseEnv()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logging.SetupLogger(envData.LogLevel)
	logger := logging.Logger.Named("spike-fleet")

	logger.Info("starting spike fleet")

	config, err := github.NewGitHubConfig(envData.GitHubURL)
	if err != nil {
		panic(err)
	}

	actionsClient, err := actions.NewActionsClient(ctx, envData, config)
	if err != nil {
		panic(err)
	}

	orkaClient, err := orka.NewOrkaClient(envData, ctx)
	if err != nil {
		panic(fmt.Sprintf("unable to access Orka cluster. More info: %s", err.Error()))
	}

	vmTracker := runners.NewVMTracker(orkaClient, actionsClient, logger)
	go vmTracker.Start(ctx, envData.VMTrackerInterval)

	ownershipPrefix := fmt.Sprintf("orka-%s-", envData.OrkaNamespace)
	running := map[string]*runningMember{}

	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	logger.Infof("starting fleet reconcile loop (every %s, discovering %q* VM configs)", reconcileInterval, vmConfigPrefix)
	for {
		if err := reconcile(ctx, actionsClient, orkaClient, envData, vmTracker, ownershipPrefix, running, logger); err != nil {
			logger.Errorf("reconcile pass failed: %s", err.Error())
		}

		select {
		case <-ctx.Done():
			shutdown(ctx, actionsClient, running, logger)
			return
		case <-ticker.C:
		}
	}
}

// reconcile syncs the running fleet to the live set of `gha-*` VM configs: it starts a
// goroutine (and creates the scale set if missing) for each newly-seen config, and stops
// the goroutine (and deletes the scale set) for each config that has disappeared. It only
// ever touches scale sets under ownershipPrefix. The running map is owned by the caller's
// single loop goroutine, so it needs no locking.
func reconcile(ctx context.Context, actionsClient *actions.ActionsClient, orkaClient orka.OrkaService, envData *env.Data, vmTracker *runners.VMTracker, ownershipPrefix string, running map[string]*runningMember, logger *zap.SugaredLogger) error {
	vmConfigs, err := orkaClient.ListVMConfigs(ctx)
	if err != nil {
		return fmt.Errorf("listing vm configs: %w", err)
	}

	// desired: scale-set name -> vm config name, for every gha-* config.
	desired := map[string]string{}
	for _, vmConfig := range vmConfigs {
		if strings.HasPrefix(vmConfig.Name, vmConfigPrefix) {
			desired[ownershipPrefix+vmConfig.Name] = vmConfig.Name
		}
	}

	actual, err := actionsClient.ListRunnerScaleSets(ctx, constants.DefaultRunnerGroupID)
	if err != nil {
		return fmt.Errorf("listing scale sets: %w", err)
	}
	actualByName := map[string]*types.RunnerScaleSet{}
	for i := range actual {
		actualByName[actual[i].Name] = &actual[i]
	}

	// Start a goroutine for each newly-desired config, creating the scale set if it doesn't
	// already exist on GitHub (e.g. adopting one left by a previous controller process).
	for name, vmConfig := range desired {
		if _, ok := running[name]; ok {
			continue
		}

		scaleSet, exists := actualByName[name]
		if !exists {
			logger.Infof("vm config %q appeared, creating scale set %s", vmConfig, name)
			scaleSet, err = actionsClient.CreateRunnerScaleSet(ctx, &types.RunnerScaleSet{
				Name:          name,
				RunnerGroupId: constants.DefaultRunnerGroupID,
				Labels: []types.RunnerScaleSetLabel{
					// Migration-parity aliases (06-multilabel-model-and-compat.md). Alias
					// ambiguity is an open probe (spike item 5) - not resolved here.
					{Name: "self-hosted", Type: "System"},
					{Name: "orka", Type: "System"},
					{Name: vmConfig, Type: "System"},
				},
				RunnerSetting: types.RunnerScaleSetSetting{
					Ephemeral:     true,
					DisableUpdate: true,
				},
			})
			if err != nil {
				logger.Errorf("creating scale set %s: %s", name, err.Error())
				continue
			}
		} else {
			logger.Infof("adopting existing scale set %s (id=%d)", name, scaleSet.Id)
		}

		memberCtx, cancel := context.WithCancel(ctx)
		running[name] = &runningMember{scaleSet: scaleSet, cancel: cancel}
		go runMember(memberCtx, actionsClient, orkaClient, envData, vmTracker, scaleSet, vmConfig, logger)
	}

	// Stop the goroutine and delete the scale set for each config that has disappeared.
	for name, member := range running {
		if _, ok := desired[name]; ok {
			continue
		}
		logger.Infof("vm config for %s gone, stopping runner and deleting scale set (no drain - see findings)", name)
		member.cancel()
		if err := actionsClient.DeleteRunnerScaleSet(ctx, member.scaleSet.Id); err != nil {
			logger.Errorf("deleting scale set %s: %s", name, err.Error())
		}
		delete(running, name)
	}

	// Delete owned scale sets that are neither desired nor running - leftovers from a
	// previous controller process. Never touches anything outside ownershipPrefix.
	for i := range actual {
		name := actual[i].Name
		if !strings.HasPrefix(name, ownershipPrefix) {
			continue
		}
		if _, ok := desired[name]; ok {
			continue
		}
		if _, ok := running[name]; ok {
			continue
		}
		logger.Infof("deleting orphaned owned scale set %s (id=%d)", name, actual[i].Id)
		if err := actionsClient.DeleteRunnerScaleSet(ctx, actual[i].Id); err != nil {
			logger.Errorf("deleting orphaned scale set %s: %s", name, err.Error())
		}
	}

	return nil
}

// shutdown cancels every running goroutine and deletes its scale set immediately (no
// drain, same as today's single-set cleanup). It uses a cancel-free context so the deletes
// still go through after the root context is done.
func shutdown(ctx context.Context, actionsClient *actions.ActionsClient, running map[string]*runningMember, logger *zap.SugaredLogger) {
	logger.Info("shutting down, deleting all owned scale sets (no drain - see findings)")
	deleteCtx := context.WithoutCancel(ctx)
	for name, member := range running {
		member.cancel()
		if err := actionsClient.DeleteRunnerScaleSet(deleteCtx, member.scaleSet.Id); err != nil {
			logger.Errorf("deleting scale set %s on exit: %s", name, err.Error())
		}
		delete(running, name)
	}
}

// runMember runs main.go's run() shape (session + message loop) for one scale set until
// memberCtx is cancelled - by reconcile when the backing VM config disappears, or by the
// root context on shutdown. Per-set vmconfig is threaded via a shallow envData copy (the
// shared provisioner.go is intentionally left untouched - see findings).
func runMember(memberCtx context.Context, actionsClient *actions.ActionsClient, orkaClient orka.OrkaService, envData *env.Data, vmTracker *runners.VMTracker, scaleSet *types.RunnerScaleSet, vmConfig string, logger *zap.SugaredLogger) {
	memberLogger := logger.Named(scaleSet.Name)

	runnerManager, err := runners.NewRunnerManager(memberCtx, actionsClient, scaleSet.Id)
	if err != nil {
		memberLogger.Errorf("failed to start session for %s: %s", scaleSet.Name, err.Error())
		return
	}
	defer runnerManager.Close()

	memberEnv := *envData
	memberEnv.OrkaVMConfig = vmConfig

	runnerProvisioner := provisioner.NewRunnerProvisioner(scaleSet, actionsClient, orkaClient, &memberEnv)
	runnerMessageProcessor := runners.NewRunnerMessageProcessor(memberCtx, runnerManager, runnerProvisioner, vmTracker, scaleSet)

	if err := runnerMessageProcessor.StartProcessingMessages(); err != nil {
		memberLogger.Errorf("message processing stopped for %s: %s", scaleSet.Name, err.Error())
	}
}
