package emulator

import (
	"context"
	"errors"
	"testing"

	"github.com/macstadium/orka-github-actions-integration/pkg/orka"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestEmulator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Emulator Suite")
}

func configsOf(resolution Resolution, vmConfig string) []string {
	pairing, ok := resolution.Pairings[vmConfig]
	if !ok {
		return nil
	}

	configs := make([]string, 0, len(pairing.Emulators))
	for _, ref := range pairing.Emulators {
		configs = append(configs, ref.Config)
	}

	return configs
}

var _ = Describe("ParseConfigName", func() {
	DescribeTable("splitting an emulator config name",
		func(name, vmConfig, suffix string, ok bool) {
			gotVMConfig, gotSuffix, gotOK := ParseConfigName(name)
			Expect(gotOK).To(Equal(ok))
			Expect(gotVMConfig).To(Equal(vmConfig))
			Expect(gotSuffix).To(Equal(suffix))
		},
		Entry("the bare form", "foo-emulator", "foo", "", true),
		Entry("a suffixed form", "foo-emulator-pixel-8", "foo", "pixel-8", true),
		Entry("a VM config with dashes", "foo-bar-emulator-tablet", "foo-bar", "tablet", true),
		Entry("a numeric suffix", "foo-emulator-2", "foo", "2", true),
		Entry("a suffix that repeats the token", "foo-emulator-emulator", "foo", "emulator", true),
		Entry("a plural is not the token", "foo-emulators", "", "", false),
		Entry("a trailing dash has no suffix", "foo-emulator-", "", "", false),
		Entry("a name with no token", "pixel8-api36", "", "", false),
		Entry("a name that starts with the token", "-emulator", "", "", false),
	)
})

var _ = Describe("ResolvePairings", func() {
	It("pairs the bare form", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, []string{"foo-emulator"})
		Expect(resolution.Pairings["foo"].Emulators).To(Equal([]EmulatorRef{{Config: "foo-emulator", Suffix: ""}}))
	})

	It("pairs suffixed forms sorted by suffix", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, []string{"foo-emulator-tablet", "foo-emulator-pixel"})
		Expect(configsOf(resolution, "foo")).To(Equal([]string{"foo-emulator-pixel", "foo-emulator-tablet"}))
		Expect(resolution.Pairings["foo"].Emulators[0].Suffix).To(Equal("pixel"))
	})

	It("puts the bare form ahead of suffixed forms", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, []string{"foo-emulator-b", "foo-emulator", "foo-emulator-a"})
		Expect(configsOf(resolution, "foo")).To(Equal([]string{"foo-emulator", "foo-emulator-a", "foo-emulator-b"}))
	})

	It("does not cap the number of emulators", func() {
		configs := []string{"foo-emulator-a", "foo-emulator-b", "foo-emulator-c", "foo-emulator-d", "foo-emulator-e"}
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, configs)
		Expect(resolution.Pairings["foo"].Emulators).To(HaveLen(5))
	})

	It("keeps foo and foo-bar apart", func() {
		resolution := ResolvePairings(
			[]string{"foo", "foo-bar"},
			[]string{"foo", "foo-bar"},
			[]string{"foo-emulator", "foo-bar-emulator", "foo-bar-emulator-tablet"},
		)
		Expect(configsOf(resolution, "foo")).To(Equal([]string{"foo-emulator"}))
		Expect(configsOf(resolution, "foo-bar")).To(Equal([]string{"foo-bar-emulator", "foo-bar-emulator-tablet"}))
	})

	It("does not pair foo with foo-bar's configs when only foo is served", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo", "foo-bar"}, []string{"foo-bar-emulator"})
		Expect(resolution.Pairings).ToNot(HaveKey("foo"))
		Expect(resolution.Unserved).To(Equal([]string{"foo-bar-emulator"}))
	})

	It("skips served VM configs whose name contains the token", func() {
		resolution := ResolvePairings([]string{"foo-emulator"}, []string{"foo-emulator"}, []string{"foo-emulator-emulator"})
		Expect(resolution.Pairings).To(BeEmpty())
		Expect(resolution.Skipped).To(Equal([]string{"foo-emulator"}))
	})

	It("reports a config whose prefix matches no VM config in the cluster as dangling", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo", "bar"}, []string{"fo-emulator", "foo-emulator"})
		Expect(resolution.Dangling).To(Equal([]string{"fo-emulator"}))
		Expect(resolution.Unserved).To(BeEmpty())
	})

	It("reports a config for another runner's VM config as unserved, not dangling", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo", "bar"}, []string{"bar-emulator"})
		Expect(resolution.Unserved).To(Equal([]string{"bar-emulator"}))
		Expect(resolution.Dangling).To(BeEmpty())
	})

	It("pairs a served VM config even when the cluster list omits it", func() {
		resolution := ResolvePairings([]string{"foo"}, nil, []string{"foo-emulator"})
		Expect(configsOf(resolution, "foo")).To(Equal([]string{"foo-emulator"}))
	})

	It("ignores configs that do not follow the convention", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, []string{"pixel8-api36", "foo-emulators"})
		Expect(resolution.Pairings).To(BeEmpty())
		Expect(resolution.Dangling).To(BeEmpty())
		Expect(resolution.Unserved).To(BeEmpty())
	})

	It("produces no pairings when there are no emulator configs", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, nil)
		Expect(resolution.Pairings).To(BeEmpty())
	})

	It("counts a repeated config name once", func() {
		resolution := ResolvePairings([]string{"foo"}, []string{"foo"}, []string{"foo-emulator", "foo-emulator"})
		Expect(resolution.Pairings["foo"].Emulators).To(HaveLen(1))
	})
})

type fakeLister struct {
	configs      []string
	vmConfigs    []string
	configsErr   error
	vmConfigsErr error
}

func (f *fakeLister) ListEmulatorConfigs(ctx context.Context) ([]*orka.OrkaEmulatorConfigResponseModel, error) {
	if f.configsErr != nil {
		return nil, f.configsErr
	}

	out := make([]*orka.OrkaEmulatorConfigResponseModel, 0, len(f.configs))
	for _, name := range f.configs {
		out = append(out, &orka.OrkaEmulatorConfigResponseModel{Name: name})
	}

	return out, nil
}

func (f *fakeLister) ListVMConfigs(ctx context.Context) ([]*orka.OrkaVMConfigResponseModel, error) {
	if f.vmConfigsErr != nil {
		return nil, f.vmConfigsErr
	}

	out := make([]*orka.OrkaVMConfigResponseModel, 0, len(f.vmConfigs))
	for _, name := range f.vmConfigs {
		out = append(out, &orka.OrkaVMConfigResponseModel{Name: name})
	}

	return out, nil
}

type fakeRecorder struct {
	calls []int
}

func (f *fakeRecorder) SetEmulatorDanglingConfigs(count int) {
	f.calls = append(f.calls, count)
}

var _ = Describe("Resolver", func() {
	var (
		lister   *fakeLister
		recorder *fakeRecorder
		logs     *observer.ObservedLogs
		resolver *Resolver
		ctx      context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		lister = &fakeLister{vmConfigs: []string{"foo", "bar"}}
		recorder = &fakeRecorder{}

		var core zapcore.Core
		core, logs = observer.New(zapcore.DebugLevel)
		resolver = NewResolver(lister, []string{"foo"}, recorder, zap.New(core).Sugar())
	})

	warnings := func() []string {
		var messages []string
		for _, entry := range logs.FilterLevelExact(zapcore.WarnLevel).All() {
			messages = append(messages, entry.Message)
		}

		return messages
	}

	It("reports nothing and is unsupported before any pass", func() {
		Expect(resolver.Supported()).To(BeFalse())
		Expect(resolver.Emulators("foo")).To(BeNil())
	})

	It("serves the pairing from the latest pass", func() {
		lister.configs = []string{"foo-emulator", "foo-emulator-tablet"}
		Expect(resolver.Refresh(ctx)).To(Succeed())

		Expect(resolver.Supported()).To(BeTrue())
		Expect(resolver.Emulators("foo")).To(Equal([]EmulatorRef{
			{Config: "foo-emulator", Suffix: ""},
			{Config: "foo-emulator-tablet", Suffix: "tablet"},
		}))
	})

	It("picks up a config added after startup on the next pass", func() {
		Expect(resolver.Refresh(ctx)).To(Succeed())
		Expect(resolver.Emulators("foo")).To(BeEmpty())

		lister.configs = []string{"foo-emulator"}
		Expect(resolver.Refresh(ctx)).To(Succeed())
		Expect(resolver.Emulators("foo")).To(HaveLen(1))
	})

	It("stays inert with a single warning when the cluster cannot list emulator configs", func() {
		lister.configsErr = errors.New("unknown command \"emulator-config\"")

		Expect(resolver.Refresh(ctx)).ToNot(Succeed())
		Expect(resolver.Refresh(ctx)).ToNot(Succeed())
		Expect(resolver.Refresh(ctx)).ToNot(Succeed())

		Expect(resolver.Supported()).To(BeFalse())
		Expect(resolver.Emulators("foo")).To(BeNil())
		Expect(warnings()).To(HaveLen(1))
		Expect(warnings()[0]).To(ContainSubstring("stay disabled"))
	})

	It("keeps the last good snapshot when a later pass fails", func() {
		lister.configs = []string{"foo-emulator"}
		Expect(resolver.Refresh(ctx)).To(Succeed())

		lister.configsErr = errors.New("connection refused")
		Expect(resolver.Refresh(ctx)).ToNot(Succeed())

		Expect(resolver.Supported()).To(BeTrue())
		Expect(resolver.Emulators("foo")).To(Equal([]EmulatorRef{{Config: "foo-emulator", Suffix: ""}}))
		Expect(warnings()).To(ContainElement(ContainSubstring("keeping the last resolved")))
	})

	It("warns again when an error recurs after a recovery", func() {
		lister.configsErr = errors.New("connection refused")
		Expect(resolver.Refresh(ctx)).ToNot(Succeed())

		lister.configsErr = nil
		Expect(resolver.Refresh(ctx)).To(Succeed())

		lister.configsErr = errors.New("connection refused")
		Expect(resolver.Refresh(ctx)).ToNot(Succeed())

		Expect(warnings()).To(HaveLen(2))
	})

	It("warns about dangling configs only when the set changes", func() {
		lister.configs = []string{"fo-emulator"}
		Expect(resolver.Refresh(ctx)).To(Succeed())
		Expect(resolver.Refresh(ctx)).To(Succeed())
		Expect(resolver.Refresh(ctx)).To(Succeed())

		Expect(warnings()).To(HaveLen(1))
		Expect(warnings()[0]).To(ContainSubstring("fo-emulator"))
		Expect(recorder.calls).To(Equal([]int{1, 1, 1}))

		lister.configs = []string{"fo-emulator", "fooo-emulator"}
		Expect(resolver.Refresh(ctx)).To(Succeed())
		Expect(warnings()).To(HaveLen(2))
	})

	It("does not warn about configs another runner serves", func() {
		lister.configs = []string{"bar-emulator"}
		Expect(resolver.Refresh(ctx)).To(Succeed())

		Expect(warnings()).To(BeEmpty())
		Expect(recorder.calls).To(Equal([]int{0}))
	})

	It("reports nothing as dangling when the VM config list has never succeeded", func() {
		lister.configs = []string{"bar-emulator", "foo-emulator"}
		lister.vmConfigsErr = errors.New("forbidden")
		Expect(resolver.Refresh(ctx)).To(Succeed())

		Expect(resolver.Emulators("foo")).To(HaveLen(1))
		Expect(warnings()).To(HaveLen(1))
		Expect(warnings()[0]).To(ContainSubstring("unable to list VM configs"))
		Expect(recorder.calls).To(BeEmpty())
	})

	It("reuses the last VM config list when a later one fails", func() {
		lister.configs = []string{"fo-emulator"}
		Expect(resolver.Refresh(ctx)).To(Succeed())

		lister.vmConfigsErr = errors.New("forbidden")
		Expect(resolver.Refresh(ctx)).To(Succeed())

		Expect(recorder.calls).To(Equal([]int{1, 1}))
	})
})
