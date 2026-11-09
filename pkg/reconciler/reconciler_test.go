package reconciler

import (
	"context"
	"errors"
	"testing"

	"github.com/macstadium/orka-github-actions-integration/pkg/logging"
	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reconciler Suite")
}

type sweepOrka struct {
	emulators    []*orka.OrkaEmulatorResponseModel
	emulatorsErr error
	vms          []*orka.OrkaVMInfo
	deleteErrs   map[string]error
	deleted      []string
	listVMCalls  int
}

type orphanCount struct {
	reaped int
}

func (o *orphanCount) AddEmulatorOrphansReaped(count int) {
	o.reaped += count
}

func (s *sweepOrka) DeployVM(ctx context.Context, namePrefix, vmConfig string) (*orka.OrkaVMDeployResponseModel, error) {
	return nil, nil
}
func (s *sweepOrka) DeleteVM(ctx context.Context, name string) error { return nil }
func (s *sweepOrka) ListVMs(ctx context.Context, namePrefix string) ([]*orka.OrkaVMInfo, error) {
	s.listVMCalls++
	return s.vms, nil
}
func (s *sweepOrka) DeployEmulator(ctx context.Context, name, vmName, config string) (*orka.OrkaEmulatorResponseModel, error) {
	return nil, nil
}
func (s *sweepOrka) DeleteEmulator(ctx context.Context, name string) error {
	s.deleted = append(s.deleted, name)
	return s.deleteErrs[name]
}
func (s *sweepOrka) ListEmulators(ctx context.Context) ([]*orka.OrkaEmulatorResponseModel, error) {
	return s.emulators, s.emulatorsErr
}
func (s *sweepOrka) ListEmulatorConfigs(ctx context.Context) ([]*orka.OrkaEmulatorConfigResponseModel, error) {
	return nil, nil
}
func (s *sweepOrka) ListVMConfigs(ctx context.Context) ([]*orka.OrkaVMConfigResponseModel, error) {
	return nil, nil
}

func emu(name, vm string) *orka.OrkaEmulatorResponseModel {
	return &orka.OrkaEmulatorResponseModel{Name: name, VM: vm}
}

var _ = Describe("Emulator orphan sweep", func() {
	var (
		fake    *sweepOrka
		counter *orphanCount
		r       *VMReconciler
	)

	BeforeEach(func() {
		logging.SetupLogger("info")
		fake = &sweepOrka{}
		counter = &orphanCount{}
		r = &VMReconciler{orkaClient: fake, metrics: counter, logger: logging.Logger.Named("test")}
	})

	It("counts only the orphans it actually deleted", func() {
		fake.emulators = []*orka.OrkaEmulatorResponseModel{
			emu("foo-abcde-emu-1", "foo-abcde"),
			emu("foo-abcde-emu-2", "foo-abcde"),
			emu("foo-fghij-emu-1", "foo-fghij"),
		}
		fake.deleteErrs = map[string]error{"foo-abcde-emu-2": errors.New("connection refused")}

		r.ReconcileVMs(context.Background(), nil, "foo")

		Expect(fake.deleted).To(Equal([]string{"foo-abcde-emu-1", "foo-abcde-emu-2", "foo-fghij-emu-1"}))
		Expect(counter.reaped).To(Equal(2))
	})

	It("deletes emulators whose paired VM in this scale set is gone", func() {
		fake.emulators = []*orka.OrkaEmulatorResponseModel{
			emu("foo-abcde-emu-1", "foo-abcde"),
			emu("foo-zzzzz-emu-1", "foo-zzzzz"),
		}
		fake.vms = []*orka.OrkaVMInfo{{Name: "foo-zzzzz"}}

		r.ReconcileVMs(context.Background(), nil, "foo")

		Expect(fake.deleted).To(Equal([]string{"foo-abcde-emu-1"}))
	})

	It("never selects another scale set's emulators that share a prefix", func() {
		fake.emulators = []*orka.OrkaEmulatorResponseModel{
			emu("foo-bar-abcde-emu-1", "foo-bar-abcde"),
			emu("foobar-abcde-emu-1", "foobar-abcde"),
			emu("manual-emu", "someones-vm"),
		}

		r.ReconcileVMs(context.Background(), nil, "foo")

		Expect(fake.deleted).To(BeEmpty())
	})

	It("skips the sweep and carries on when the cluster cannot list emulators", func() {
		fake.emulatorsErr = errors.New("unknown command \"emulator\"")

		Expect(func() { r.ReconcileVMs(context.Background(), nil, "foo") }).ToNot(Panic())
		Expect(fake.listVMCalls).To(Equal(0))
		Expect(fake.deleted).To(BeEmpty())
	})

	It("does not list VMs when there are no emulators at all", func() {
		r.ReconcileVMs(context.Background(), nil, "foo")
		Expect(fake.listVMCalls).To(Equal(0))
	})
})
