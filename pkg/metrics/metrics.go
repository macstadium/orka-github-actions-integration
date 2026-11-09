package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/actions"
	"github.com/macstadium/orka-github-actions-integration/pkg/github/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Results recorded by RecordEmulatorDeploy.
const (
	EmulatorDeploySuccess  = "success"
	EmulatorDeployCapacity = "capacity"
	EmulatorDeployError    = "error"
)

// Metrics holds every series the runner exports. A nil *Metrics is valid and records nothing, so
// callers do not need to branch on whether metrics are enabled.
type Metrics struct {
	registry   *prometheus.Registry
	runnerName string

	totalAvailableJobs     *prometheus.GaugeVec
	totalAcquiredJobs      *prometheus.GaugeVec
	totalAssignedJobs      *prometheus.GaugeVec
	totalRunningJobs       *prometheus.GaugeVec
	totalRegisteredRunners *prometheus.GaugeVec
	totalBusyRunners       *prometheus.GaugeVec
	totalIdleRunners       *prometheus.GaugeVec

	emulatorDeploys          *prometheus.CounterVec
	emulatorDeployDuration   *prometheus.HistogramVec
	emulatorOrphansReaped    *prometheus.CounterVec
	emulatorDanglingConfigs  *prometheus.GaugeVec
	emulatorCapacityBreaker  *prometheus.GaugeVec
	runnerProvisionExhausted *prometheus.CounterVec
}

// New builds the series without starting the HTTP server or the stats poller, which Start adds.
func New(runnerName string) *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry:   registry,
		runnerName: runnerName,

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

		emulatorDeploys: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "emulator_deploy_total",
			Help: "Android emulator deploys, by result: success, capacity (the node had no room for another emulator), or error",
		}, []string{"runner_name", "result"}),

		emulatorDeployDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "emulator_deploy_duration_seconds",
			Help: "Time taken by each Android emulator deploy, whatever its result",
			// A warm deploy takes seconds; a cold one waits on a system image pull for minutes.
			Buckets: []float64{5, 10, 20, 30, 60, 120, 300, 600},
		}, []string{"runner_name"}),

		emulatorOrphansReaped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "emulator_orphans_reaped_total",
			Help: "Android emulators deleted at startup because their paired VM no longer exists",
		}, []string{"runner_name"}),

		emulatorDanglingConfigs: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "emulator_dangling_configs",
			Help: "Android emulator configs that will never be deployed, because their name does not follow the naming convention or matches no VM config in the cluster",
		}, []string{"runner_name"}),

		emulatorCapacityBreaker: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "emulator_capacity_breaker_open",
			Help: "1 while only one job at a time is provisioned because attempts keep failing for lack of Android emulator capacity",
		}, []string{"runner_name"}),

		runnerProvisionExhausted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "runner_provision_exhausted_total",
			Help: "Rounds of provisioning attempts for a job that all failed before GitHub reissued the job",
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
		m.emulatorDeploys,
		m.emulatorDeployDuration,
		m.emulatorOrphansReaped,
		m.emulatorDanglingConfigs,
		m.emulatorCapacityBreaker,
		m.runnerProvisionExhausted,
	)

	return m
}

// RecordEmulatorDeploy counts one emulator deploy and observes how long it took.
func (m *Metrics) RecordEmulatorDeploy(result string, duration time.Duration) {
	if m == nil {
		return
	}

	m.emulatorDeploys.WithLabelValues(m.runnerName, result).Inc()
	m.emulatorDeployDuration.WithLabelValues(m.runnerName).Observe(duration.Seconds())
}

// AddEmulatorOrphansReaped counts emulators deleted by the startup orphan sweep.
func (m *Metrics) AddEmulatorOrphansReaped(count int) {
	if m == nil || count <= 0 {
		return
	}

	m.emulatorOrphansReaped.WithLabelValues(m.runnerName).Add(float64(count))
}

// SetEmulatorDanglingConfigs records the dangling config count from the latest resolution pass.
func (m *Metrics) SetEmulatorDanglingConfigs(count int) {
	if m == nil {
		return
	}

	m.emulatorDanglingConfigs.WithLabelValues(m.runnerName).Set(float64(count))
}

// SetEmulatorCapacityBreakerOpen records whether the emulator capacity breaker is open.
func (m *Metrics) SetEmulatorCapacityBreakerOpen(open bool) {
	if m == nil {
		return
	}

	value := 0.0
	if open {
		value = 1
	}

	m.emulatorCapacityBreaker.WithLabelValues(m.runnerName).Set(value)
}

// IncRunnerProvisionExhausted counts a job given up on after the provisioning retry limit.
func (m *Metrics) IncRunnerProvisionExhausted() {
	if m == nil {
		return
	}

	m.runnerProvisionExhausted.WithLabelValues(m.runnerName).Inc()
}

func Start(
	ctx context.Context,
	logger *zap.SugaredLogger,
	envData *env.Data,
	actionsClient actions.ActionsService,
	runnerName string,
	groupId int,
) *Metrics {

	m := New(runnerName)

	// Start HTTP server
	go m.startServer(ctx, logger, envData.MetricsAddr)

	// Start poller
	go m.startPoller(ctx, logger, envData.MetricsPollInterval, actionsClient, runnerName, groupId)

	return m
}

func (m *Metrics) startServer(ctx context.Context, logger *zap.SugaredLogger, addr string) {
	server := &http.Server{
		Addr:    addr,
		Handler: promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}),
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	logger.Infof("Prometheus metrics available at %s/metrics", addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Errorf("metrics server failed: %v", err)
	}
}

func (m *Metrics) startPoller(
	ctx context.Context,
	logger *zap.SugaredLogger,
	interval time.Duration,
	actionsClient actions.ActionsService,
	runnerName string,
	groupId int,
) {
	ticker := time.NewTicker(interval)
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
				continue
			}

			m.update(runnerName, scaleSet.Statistics)
		}
	}
}

func (m *Metrics) update(runnerName string, stats *types.RunnerScaleSetStatistic) {
	if stats == nil {
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
