package metrics

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dto "github.com/prometheus/client_model/go"
)

func TestMetrics(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Suite")
}

// sample finds the series of a family whose labels include every given label, and returns its
// counter, gauge, or histogram sample count as a float.
func sample(m *Metrics, family string, labels map[string]string) float64 {
	families, err := m.registry.Gather()
	Expect(err).ToNot(HaveOccurred())

	for _, f := range families {
		if f.GetName() != family {
			continue
		}

		for _, metric := range f.GetMetric() {
			if !hasLabels(metric, labels) {
				continue
			}

			switch {
			case metric.Counter != nil:
				return metric.GetCounter().GetValue()
			case metric.Gauge != nil:
				return metric.GetGauge().GetValue()
			case metric.Histogram != nil:
				return float64(metric.GetHistogram().GetSampleCount())
			}
		}
	}

	Fail("no series " + family + " with the given labels")
	return 0
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	matched := 0
	for _, pair := range metric.GetLabel() {
		if value, ok := labels[pair.GetName()]; ok && value == pair.GetValue() {
			matched++
		}
	}

	return matched == len(labels)
}

var _ = Describe("Metrics", func() {
	It("records nothing and does not panic when nil", func() {
		var m *Metrics
		Expect(func() {
			m.RecordEmulatorDeploy(EmulatorDeploySuccess, time.Second)
			m.AddEmulatorOrphansReaped(2)
			m.SetEmulatorDanglingConfigs(1)
			m.SetEmulatorCapacityBreakerOpen(true)
		}).ToNot(Panic())
	})

	It("labels the emulator series with the runner name and result", func() {
		m := New("my-runner")
		runner := map[string]string{"runner_name": "my-runner"}

		m.RecordEmulatorDeploy(EmulatorDeploySuccess, 6*time.Second)
		m.RecordEmulatorDeploy(EmulatorDeploySuccess, 7*time.Second)
		m.RecordEmulatorDeploy(EmulatorDeployCapacity, 30*time.Second)
		m.AddEmulatorOrphansReaped(3)
		m.SetEmulatorDanglingConfigs(2)
		m.SetEmulatorCapacityBreakerOpen(true)

		Expect(sample(m, "emulator_deploy_total", map[string]string{"runner_name": "my-runner", "result": EmulatorDeploySuccess})).To(Equal(2.0))
		Expect(sample(m, "emulator_deploy_total", map[string]string{"runner_name": "my-runner", "result": EmulatorDeployCapacity})).To(Equal(1.0))
		Expect(sample(m, "emulator_deploy_duration_seconds", runner)).To(Equal(3.0))
		Expect(sample(m, "emulator_orphans_reaped_total", runner)).To(Equal(3.0))
		Expect(sample(m, "emulator_dangling_configs", runner)).To(Equal(2.0))
		Expect(sample(m, "emulator_capacity_breaker_open", runner)).To(Equal(1.0))

		m.SetEmulatorCapacityBreakerOpen(false)
		Expect(sample(m, "emulator_capacity_breaker_open", runner)).To(Equal(0.0))
	})
})
