package orka

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOrka(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Orka Suite")
}

func emulatorFor(name, vm string) *OrkaEmulatorResponseModel {
	return &OrkaEmulatorResponseModel{Name: name, VM: vm}
}

func names(emulators []*OrkaEmulatorResponseModel) []string {
	out := make([]string, 0, len(emulators))
	for _, emulator := range emulators {
		out = append(out, emulator.Name)
	}

	return out
}

var _ = Describe("Orka Test", func() {

	DescribeTable("recognizing an emulator that is already gone",
		func(err error, expected bool) {
			Expect(isEmulatorNotFound(err, "foo-abcde-emu-1")).To(Equal(expected))
		},
		Entry("the NotFound the CLI prints, quoted into the exec error",
			fmt.Errorf("command 'orka emulator delete foo-abcde-emu-1 --namespace orka-default' failed with output: %q, error: %v",
				"Error: androidemulatorinstances.orka.macstadium.com \"foo-abcde-emu-1\" not found\n", "exit status 1"),
			true),
		Entry("the NotFound unquoted",
			errors.New(`Error: androidemulatorinstances.orka.macstadium.com "foo-abcde-emu-1" not found`),
			true),
		Entry("a missing CLI binary",
			errors.New(`command 'orka emulator delete foo-abcde-emu-1 --namespace orka-default' failed with output: "", error: exec: "orka": executable file not found in $PATH`),
			false),
		Entry("a NotFound for another emulator",
			errors.New(`Error: androidemulatorinstances.orka.macstadium.com "foo-abcde-emu-12" not found`),
			false),
		Entry("a NotFound for something else",
			errors.New(`Error: namespaces "orka-default" not found`),
			false),
	)

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

	// Captured from 'orka emulator deploy -o json' against an Orka 4.0.0 cluster.
	It("decodes the emulator deploy response the CLI returns", func() {
		raw := `[{"age":"6s","deviceProfile":null,"grpcPort":null,"imageType":"google_apis","name":"ok6365-verify-emu-1","node":"mini-arm-36","owner":"kubernetes-admin","platform":"android-36","relayIp":"192.168.64.1","relayPort":15555,"status":"Running","vm":"ok6365-verify"}]`

		var decoded []*OrkaEmulatorResponseModel
		Expect(json.Unmarshal([]byte(raw), &decoded)).To(Succeed())
		Expect(decoded).To(HaveLen(1))

		emulator := decoded[0]
		Expect(emulator.Name).To(Equal("ok6365-verify-emu-1"))
		Expect(emulator.VM).To(Equal("ok6365-verify"))
		Expect(emulator.Status).To(Equal(EmulatorRunning))
		Expect(emulator.DeviceProfile).To(BeNil())
		Expect(emulator.ADBTarget()).To(Equal("192.168.64.1:15555"))
	})

	Describe("selecting the emulators paired with a VM", func() {
		emulators := []*OrkaEmulatorResponseModel{
			emulatorFor("foo-abcde-emu-1", "foo-abcde"),
			emulatorFor("foo-abcde-emu-2", "foo-abcde"),
			emulatorFor("foo-abcde2-emu-1", "foo-abcde2"),
			emulatorFor("foo-bar-abcde-emu-1", "foo-bar-abcde"),
			nil,
		}

		It("matches the VM name exactly", func() {
			Expect(names(EmulatorsForVM(emulators, "foo-abcde"))).To(Equal([]string{"foo-abcde-emu-1", "foo-abcde-emu-2"}))
		})

		It("finds nothing for a VM without emulators", func() {
			Expect(EmulatorsForVM(emulators, "foo-zzzzz")).To(BeEmpty())
		})
	})

	DescribeTable("deciding whether a VM belongs to a scale set",
		func(vmName, scaleSetName string, expected bool) {
			Expect(BelongsToScaleSet(vmName, scaleSetName)).To(Equal(expected))
		},
		Entry("a generated name belongs", "foo-abcde", "foo", true),
		Entry("a longer scale set's VM does not belong to the shorter one", "foo-bar-abcde", "foo", false),
		Entry("the longer scale set owns its own VM", "foo-bar-abcde", "foo-bar", true),
		Entry("a name sharing only a prefix does not belong", "foobar-abcde", "foo", false),
		Entry("the bare scale set name is not a VM of it", "foo", "foo", false),
		Entry("a trailing dash with no suffix does not belong", "foo-", "foo", false),
	)
})
