// Spike (OK-5518, Track A): N scale sets managed by one controller process.
// Not wired into `make build`/`make run` - a separate binary, run manually against a
// real GitHub org + Orka cluster. Findings: docs/04-spike-findings.md (spike doc series).
//
// Proves: reconcile-by-listing (needs the new ListRunnerScaleSets client method),
// ownership-prefix scoping, per-set alias labels, and running N independent
// RunnerManager/RunnerProvisioner/RunnerMessageProcessor triples as goroutines in one
// process - the same per-set primitives main.go already uses for a single set.
//
// NOT implemented here (see findings for why): drain-on-delete (deletes immediately,
// same as today's single-set ManageRunnerScaleSets cleanup), stale-session recreate
// (main.go's ErrActiveSession handling - orthogonal to the fleet question, reuse as-is),
// real vmconfig discovery (hardcoded desired list stands in for `orka3 vmc list -o json`
// filtered by "gha-" prefix, confirmed to exist in orka-cli-v2 but not called from here).
package main

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"sync"
	"syscall"

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

// fleetMember is a stand-in for one discovered `gha-*` vmconfig. Name and VMConfig are
// the same string in the real convention (the vmconfig name IS the image identity).
type fleetMember struct {
	Name     string
	VMConfig string
}

func main() {
	envData := env.ParseEnv()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logging.SetupLogger(envData.LogLevel)
	logger := logging.Logger.Named("spike-fleet")

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

	// Stand-in for `orka3 vmc list -o json` filtered to a "gha-" prefix.
	desired := []fleetMember{
		{Name: "gha-xcode-16", VMConfig: "gha-xcode-16"},
		{Name: "gha-xcode-15", VMConfig: "gha-xcode-15"},
	}

	ownershipPrefix := fmt.Sprintf("orka-%s-", envData.OrkaNamespace)

	scaleSets, err := reconcile(ctx, actionsClient, ownershipPrefix, constants.DefaultRunnerGroupID, desired, logger)
	if err != nil {
		panic(fmt.Sprintf("reconcile failed: %s", err.Error()))
	}

	vmTracker := runners.NewVMTracker(orkaClient, actionsClient, logger)
	go vmTracker.Start(ctx, envData.VMTrackerInterval)

	var wg sync.WaitGroup
	for _, member := range scaleSets {
		wg.Add(1)
		go runMember(ctx, &wg, actionsClient, orkaClient, envData, vmTracker, member.scaleSet, member.vmConfig, logger)
	}

	go func() {
		<-ctx.Done()
		if ctx.Err() == context.Canceled {
			logger.Info("received termination signal, deleting owned scale sets (no drain - see findings)")
			for _, member := range scaleSets {
				if err := actionsClient.DeleteRunnerScaleSet(context.WithoutCancel(ctx), member.scaleSet.Id); err != nil {
					logger.Errorf("error deleting scale set %s on exit: %s", member.scaleSet.Name, err.Error())
				}
			}
		}
	}()

	wg.Wait()
}

type activeMember struct {
	scaleSet *types.RunnerScaleSet
	vmConfig string
}

// reconcile lists our owned scale sets (ownershipPrefix), creates the missing desired
// ones, and deletes owned-but-undesired ones. Never touches a scale set outside the
// prefix. Returns the full set that should be running after this pass.
func reconcile(ctx context.Context, actionsClient *actions.ActionsClient, ownershipPrefix string, groupID int, desired []fleetMember, logger *zap.SugaredLogger) ([]activeMember, error) {
	actual, err := actionsClient.ListRunnerScaleSets(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("listing scale sets: %w", err)
	}

	actualByName := map[string]*types.RunnerScaleSet{}
	for i := range actual {
		actualByName[actual[i].Name] = &actual[i]
	}

	desiredNames := map[string]bool{}
	active := make([]activeMember, 0, len(desired))

	for _, member := range desired {
		name := ownershipPrefix + member.Name
		desiredNames[name] = true

		scaleSet, exists := actualByName[name]
		if !exists {
			logger.Infof("creating scale set %s", name)
			scaleSet, err = actionsClient.CreateRunnerScaleSet(ctx, &types.RunnerScaleSet{
				Name:          name,
				RunnerGroupId: groupID,
				Labels: []types.RunnerScaleSetLabel{
					// Migration-parity aliases (06-multilabel-model-and-compat.md). Alias
					// ambiguity is an open probe (spike item 5) - not resolved here.
					{Name: "self-hosted", Type: "System"},
					{Name: "orka", Type: "System"},
					{Name: member.Name, Type: "System"},
				},
				RunnerSetting: types.RunnerScaleSetSetting{
					Ephemeral:     true,
					DisableUpdate: true,
				},
			})
			if err != nil {
				return nil, fmt.Errorf("creating scale set %s: %w", name, err)
			}
		} else {
			logger.Infof("reusing existing scale set %s (id=%d)", name, scaleSet.Id)
		}

		active = append(active, activeMember{scaleSet: scaleSet, vmConfig: member.VMConfig})
	}

	for _, scaleSet := range actual {
		if strings.HasPrefix(scaleSet.Name, ownershipPrefix) && !desiredNames[scaleSet.Name] {
			logger.Infof("deleting unbacked owned scale set %s (id=%d)", scaleSet.Name, scaleSet.Id)
			if err := actionsClient.DeleteRunnerScaleSet(ctx, scaleSet.Id); err != nil {
				logger.Errorf("error deleting unbacked scale set %s: %s", scaleSet.Name, err.Error())
			}
		}
	}

	return active, nil
}

// runMember is main.go's run() (session + message loop) for exactly one scale set,
// with a per-member vmconfig instead of the process-wide envData.OrkaVMConfig.
// Proves item 1: N of these running concurrently as goroutines from one process.
func runMember(ctx context.Context, wg *sync.WaitGroup, actionsClient *actions.ActionsClient, orkaClient orka.OrkaService, envData *env.Data, vmTracker *runners.VMTracker, scaleSet *types.RunnerScaleSet, vmConfig string, logger *zap.SugaredLogger) {
	defer wg.Done()

	memberLogger := logger.Named(scaleSet.Name)

	runnerManager, err := runners.NewRunnerManager(ctx, actionsClient, scaleSet.Id)
	if err != nil {
		memberLogger.Errorf("failed to start session for %s: %s", scaleSet.Name, err.Error())
		return
	}
	defer runnerManager.Close()

	// RunnerProvisioner reads envData.OrkaVMConfig directly (today: one global value for
	// the whole process). A real fleet controller needs per-set sizing, so we hand each
	// member a shallow copy with only OrkaVMConfig swapped - proves the shape of the
	// change without touching the shared provisioner.go.
	memberEnv := *envData
	memberEnv.OrkaVMConfig = vmConfig

	runnerProvisioner := provisioner.NewRunnerProvisioner(scaleSet, actionsClient, orkaClient, &memberEnv)
	runnerMessageProcessor := runners.NewRunnerMessageProcessor(ctx, runnerManager, runnerProvisioner, vmTracker, scaleSet)

	if err := runnerMessageProcessor.StartProcessingMessages(); err != nil {
		memberLogger.Errorf("message processing stopped for %s: %s", scaleSet.Name, err.Error())
	}
}
