package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/validation"
)

var runnerScaleSetIDs = []int{}

type Metrics struct {
	totalAvailableJobs     *prometheus.GaugeVec
	totalAcquiredJobs      *prometheus.GaugeVec
	totalAssignedJobs      *prometheus.GaugeVec
	totalRunningJobs       *prometheus.GaugeVec
	totalRegisteredRunners *prometheus.GaugeVec
	totalBusyRunners       *prometheus.GaugeVec
	totalIdleRunners       *prometheus.GaugeVec
}

func newMetricsRegistry() (*prometheus.Registry, *Metrics) {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		totalAvailableJobs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_available_jobs",
			Help: "Total number of available jobs",
		}, []string{"runner_name"}),
		totalAcquiredJobs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_acquired_jobs",
			Help: "Total number of acquired jobs",
		}, []string{"runner_name"}),
		totalAssignedJobs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_assigned_jobs",
			Help: "Total number of assigned jobs",
		}, []string{"runner_name"}),
		totalRunningJobs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_running_jobs",
			Help: "Total number of running jobs",
		}, []string{"runner_name"}),
		totalRegisteredRunners: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_registered_runners",
			Help: "Total number of registered runners",
		}, []string{"runner_name"}),
		totalBusyRunners: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_busy_runners",
			Help: "Total number of busy runners",
		}, []string{"runner_name"}),
		totalIdleRunners: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "runner_scale_set_total_idle_runners",
			Help: "Total number of idle runners",
		}, []string{"runner_name"}),
	}

	registry.MustRegister(
		m.totalAvailableJobs,
		m.totalAcquiredJobs,
		m.totalAssignedJobs,
		m.totalRunningJobs,
		m.totalRegisteredRunners,
		m.totalBusyRunners,
		m.totalIdleRunners,
	)

	return registry, m
}

// Function to update metrics
func updateMetrics(m *Metrics, runnerName string, stats *types.RunnerScaleSetStatistic) {
	if m == nil || stats == nil {
		return
	}

	m.totalAvailableJobs.WithLabelValues(runnerName).Set(float64(stats.TotalAvailableJobs))
	m.totalAcquiredJobs.WithLabelValues(runnerName).Set(float64(stats.TotalAcquiredJobs))
	m.totalAssignedJobs.WithLabelValues(runnerName).Set(float64(stats.TotalAssignedJobs))
	m.totalRunningJobs.WithLabelValues(runnerName).Set(float64(stats.TotalRunningJobs))
	m.totalRegisteredRunners.WithLabelValues(runnerName).Set(float64(stats.TotalRegisteredRunners))
	m.totalBusyRunners.WithLabelValues(runnerName).Set(float64(stats.TotalBusyRunners))
	m.totalIdleRunners.WithLabelValues(runnerName).Set(float64(stats.TotalIdleRunners))
}

func main() {
	envData := env.ParseEnv()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logging.SetupLogger(envData.LogLevel)
	logger := logging.Logger.Named("main")

	config, err := github.NewGitHubConfig(envData.GitHubURL)
	if err != nil {
		panic(err)
	}

	runnerName := envData.Runners[0].Name
	groupId := constants.DefaultRunnerGroupID
	if envData.Runners[0].Id != 0 {
		groupId = envData.Runners[0].Id
	}

	if len(validation.IsValidLabelValue(runnerName)) > 0 || len(validation.IsDNS1035Label(runnerName)) > 0 {
		panic(fmt.Sprintf("invalid runner name: %s. Runner name must consist of lower case alphanumeric characters or ' - ', start with an alphabetic character, end with an alphanumeric character, and may not be longer than 63 characters.", runnerName))
	}

	actionsClient, err := actions.NewActionsClient(ctx, envData, config)
	if err != nil {
		panic(err)
	}

	// Start Prometheus metrics server
	var metrics *Metrics

	if envData.EnableMetrics {
		registry, m := newMetricsRegistry()
		metrics = m

		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

			fmt.Printf("Prometheus metrics available at %s/metrics\n", envData.MetricsAddr)

			if err := http.ListenAndServe(envData.MetricsAddr, mux); err != nil {
				logger.Fatalf("metrics server failed: %v", err)
			}
		}()
	}

	go func() {
		// Wait for termination signal
		<-ctx.Done()

		if ctx.Err() == context.Canceled {
			fmt.Println("Received termination signal. Performing cleanup...")

			for _, runnerScaleSetID := range runnerScaleSetIDs {
				err = actionsClient.DeleteRunnerScaleSet(context.TODO(), runnerScaleSetID)
				if err != nil {
					fmt.Printf("error while deleting runnerScaleSet %s", err.Error())
				}
			}

			os.Exit(0)
		}
	}()

	runnerScaleSet, err := actionsClient.CreateRunnerScaleSet(ctx, &types.RunnerScaleSet{
		Name:          runnerName,
		RunnerGroupId: groupId,
		Labels: []types.RunnerScaleSetLabel{
			{
				Name: runnerName,
				Type: "System",
			},
		},
		RunnerSetting: types.RunnerScaleSetSetting{
			Ephemeral:     true,
			DisableUpdate: true,
		},
	})
	if err != nil {
		panic(fmt.Sprintf("unable to create runner %s, err: %s", runnerName, err.Error()))
	}

	runnerScaleSetIDs = append(runnerScaleSetIDs, runnerScaleSet.Id)

	if envData.EnableMetrics {
		go func() {
			ticker := time.NewTicker(envData.MetricsPollInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					logger.Info("metrics poller shutting down")
					return

				case <-ticker.C:
					scaleSet, err := actionsClient.GetRunnerScaleSet(ctx, groupId, runnerName)
					if err != nil {
						logger.Errorf("failed to fetch runner scale set stats: %v", err)
						continue
					}

					if scaleSet == nil {
						logger.Warn("runner scale set is nil")
						continue
					}

					updateMetrics(metrics, runnerName, scaleSet.Statistics)
				}
			}
		}()
	}

	orkaClient, err := orka.NewOrkaClient(envData, ctx)
	if err != nil {
		panic(fmt.Sprintf("unable to access Orka cluster. More info: %s", err.Error()))
	}

	run(ctx, actionsClient, orkaClient, runnerScaleSet, envData, logger)
}

func run(ctx context.Context, actionsClient *actions.ActionsClient, orkaClient *orka.OrkaClient, runnerScaleSet *types.RunnerScaleSet, envData *env.Data, logger *zap.SugaredLogger) {
	runnerManager, err := runners.NewRunnerManager(ctx, actionsClient, runnerScaleSet.Id)
	if err != nil {
		panic(err)
	}
	defer runnerManager.Close()

	runnerProvisioner := provisioner.NewRunnerProvisioner(runnerScaleSet, actionsClient, orkaClient, envData)

	vmTracker := runners.NewVMTracker(orkaClient, actionsClient, logger)
	go vmTracker.Start(ctx, envData.VMTrackerInterval)

	runnerMessageProcessor := runners.NewRunnerMessageProcessor(ctx, runnerManager, runnerProvisioner, vmTracker, runnerScaleSet)

	if err = runnerMessageProcessor.StartProcessingMessages(); err != nil {
		logger.Errorf("failed to start processing messages for runnerScaleSet %s: %w", runnerScaleSet.Name, err.Error())
	}
}
