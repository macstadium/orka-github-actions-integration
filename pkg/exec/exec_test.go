package exec

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestExec(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Exec Suite")
}

var _ = Describe("Exec", func() {
	It("returns the combined output of a successful command", func() {
		out, err := ExecStringCommand(context.Background(), "sh", []string{"-c", "echo hello"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(Equal("hello\n"))
	})

	It("decodes JSON output", func() {
		res, err := ExecJSONCommand[[]map[string]string](context.Background(), "sh", []string{"-c", `echo '[{"name":"a"}]'`})
		Expect(err).ToNot(HaveOccurred())
		Expect(*res).To(Equal([]map[string]string{{"name": "a"}}))
	})

	It("includes the output of a failed command in the error", func() {
		_, err := ExecStringCommand(context.Background(), "sh", []string{"-c", "echo boom >&2; exit 3"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(errors.Is(err, context.Canceled)).To(BeFalse())
	})

	It("kills the process when the context is cancelled and reports the cancellation", func() {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)

		start := time.Now()
		_, err := ExecStringCommand(ctx, "sleep", []string{"30"})

		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})
})
