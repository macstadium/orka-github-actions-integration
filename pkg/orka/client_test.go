package orka

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOrka(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Orka Suite")
}

func emulatorConfigs(names ...string) []*OrkaEmulatorConfigResponseModel {
	configs := make([]*OrkaEmulatorConfigResponseModel, 0, len(names))
	for _, name := range names {
		configs = append(configs, &OrkaEmulatorConfigResponseModel{Name: name})
	}

	return configs
}

var _ = Describe("Orka Test", func() {

	DescribeTable("when diffing configured emulator configs against the cluster",
		func(configured []string, existing []string, expectedMissing []string) {
			missing, _ := diffEmulatorConfigs(configured, emulatorConfigs(existing...))
			Expect(missing).To(Equal(expectedMissing))
		},
		Entry("with every config present, should report none missing",
			[]string{"pixel8-api36", "tablet-api35"},
			[]string{"pixel8-api36", "tablet-api35"},
			[]string{}),
		Entry("with a config absent, should report it missing",
			[]string{"pixel8-api36", "typo-config"},
			[]string{"pixel8-api36", "tablet-api35"},
			[]string{"typo-config"}),
		Entry("with no configs in the cluster, should report all missing",
			[]string{"pixel8-api36"},
			nil,
			[]string{"pixel8-api36"}),
		Entry("with extra configs in the cluster, should report none missing",
			[]string{"pixel8-api36"},
			[]string{"pixel8-api36", "unused-config"},
			[]string{}),
		Entry("with a repeated missing config, should report it once per entry",
			[]string{"typo-config", "typo-config"},
			[]string{"pixel8-api36"},
			[]string{"typo-config", "typo-config"}),
		Entry("with nothing configured, should report none missing",
			nil,
			[]string{"pixel8-api36"},
			[]string{}),
	)

	It("reports the available config names alongside the missing ones", func() {
		_, available := diffEmulatorConfigs([]string{"typo"}, emulatorConfigs("pixel8-api36", "tablet-api35"))
		Expect(available).To(Equal([]string{"pixel8-api36", "tablet-api35"}))
	})

	Describe("the emulator relay address", func() {
		It("is unavailable when the emulator is still pending", func() {
			emulator := &OrkaEmulatorResponseModel{Status: EmulatorPending}
			Expect(emulator.HasRelay()).To(BeFalse())
			Expect(emulator.ADBTarget()).To(BeEmpty())
		})

		It("is unavailable when the relay IP is present but empty", func() {
			host := ""
			port := 15555
			emulator := &OrkaEmulatorResponseModel{RelayIP: &host, RelayPort: &port}
			Expect(emulator.HasRelay()).To(BeFalse())
		})

		It("is unavailable when the port is missing", func() {
			host := "192.168.64.1"
			emulator := &OrkaEmulatorResponseModel{RelayIP: &host}
			Expect(emulator.HasRelay()).To(BeFalse())
		})

		It("is the host and port a job inside the paired VM connects to", func() {
			host := "192.168.64.1"
			port := 15555
			emulator := &OrkaEmulatorResponseModel{RelayIP: &host, RelayPort: &port}
			Expect(emulator.HasRelay()).To(BeTrue())
			Expect(emulator.ADBTarget()).To(Equal("192.168.64.1:15555"))
		})
	})
})
