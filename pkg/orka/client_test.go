package orka

import (
	"os"
	"testing"

	"github.com/macstadium/orka-github-actions-integration/pkg/env"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOrka(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Orka Suite")
}

var _ = Describe("Orka Client Test", func() {
	Describe("when building the deploy command arguments", func() {
		It("should not include --userdata when no userdata is configured", func() {
			client := &OrkaClient{envData: &env.Data{OrkaNamespace: "orka-default"}}

			args := client.deployArgs("runner", "my-config")

			Expect(args).To(Equal([]string{"vm", "deploy", "runner", "--config", "my-config",
				"--generate-name", "-o", "json", "--namespace", "orka-default"}))
		})

		It("should include the userdata file path and not the script content", func() {
			client := &OrkaClient{
				envData:      &env.Data{OrkaNamespace: "orka-default", OrkaVMUserdata: "#!/bin/bash\necho hi"},
				userdataPath: "/private/path/orka-vm-userdata-123.sh",
			}

			args := client.deployArgs("runner", "my-config")

			Expect(args).To(ContainElements("--userdata", "/private/path/orka-vm-userdata-123.sh"))
			for _, arg := range args {
				Expect(arg).NotTo(ContainSubstring("echo hi"))
			}
		})

		It("should keep the metadata argument alongside userdata", func() {
			client := &OrkaClient{
				envData:      &env.Data{OrkaNamespace: "orka-default", OrkaVMMetadata: "key=value"},
				userdataPath: "/private/path/orka-vm-userdata-123.sh",
			}

			args := client.deployArgs("runner", "my-config")

			Expect(args).To(ContainElements("--metadata", "key=value", "--userdata", "/private/path/orka-vm-userdata-123.sh"))
		})
	})

	Describe("when writing the userdata script file", func() {
		It("should create a file readable only by the current user with the script content", func() {
			script := "#!/bin/bash\necho userdata"

			path, err := writeUserdataFile(script)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.Remove, path)

			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))

			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(script))
		})
	})
})
