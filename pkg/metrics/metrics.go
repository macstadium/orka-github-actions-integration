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

type Metrics struct {
	registry *prometheus.Registry

	totalAvailableJobs     *prometheus.GaugeVec
	totalAcquiredJobs      *prometheus.GaugeVec
	totalAssignedJobs      *prometheus.GaugeVec
	totalRunningJobs       *prometheus.GaugeVec
	totalRegisteredRunners *prometheus.GaugeVec
	totalBusyRunners       *prometheus.GaugeVec
	totalIdleRunners       *prometheus.GaugeVec
}

func newMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,

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

	return m
}

func Start(
	ctx context.Context,
	logger *zap.SugaredLogger,
	envData *env.Data,
	actionsClient *actions.ActionsClient,
	runnerName string,
	groupId int,
) *Metrics {

	m := newMetrics()

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
	actionsClient *actions.ActionsClient,
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
