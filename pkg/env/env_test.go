package env

import (
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEnv(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Env Suite")
}

var _ = Describe("Env Test", func() {

	DescribeTable("when validating the metadata env",
		func(input string, expected bool) {
			Expect(validateMetadata(input)).To(Equal(expected))
		},
		Entry("with valid string with one key-value pair, should be valid", "key1=value1", true),
		Entry("with valid string with multiple key-value pairs, should be valid", "key1=value1,key2=value2,key3=value3", true),
		Entry("with valid string with spaces after comma, should be valid", "key1=value1, key2=value2", true),
		Entry("with invalid string with missing value, should be invalid", "key1=value1,key2=", false),
		Entry("with string with missing key, should be invalid", "=value1,key2=value2", false),
		Entry("with invalid string with missing equals sign, should be invalid", "key1=value1,key2value2", false),
		Entry("with invalid string with trailing comma, should be invalid", "key1=value1,key2=value2,", false),
		Entry("with invalid empty string, should be invalid", "", false),
		Entry("with invalid string with empty key, should be invalid", "=value1", false),
		Entry("with invalid string with empty value, should be invalid", "key1=", false),
		Entry("with invalid string with no equals sign, should be invalid", "key1;value1", false),
	)

	DescribeTable("when validating the userdata size",
		func(scriptSize int, expectError bool) {
			envData := &Data{
				GitHubURL:      "https://github.com/my-org/my-repo",
				OrkaURL:        "http://10.221.188.20",
				OrkaToken:      "token",
				OrkaVMConfig:   "my-config",
				OrkaVMUserdata: strings.Repeat("a", scriptSize),
			}

			errors := validateEnv(envData)

			if expectError {
				Expect(errors).To(HaveLen(1))
				Expect(errors[0]).To(ContainSubstring("userdata script exceeds the maximum size"))
			} else {
				Expect(errors).To(BeEmpty())
			}
		},
		Entry("with an empty script, should be valid", 0, false),
		Entry("with a small script, should be valid", 100, false),
		Entry("with the largest script that encodes to 64KiB, should be valid", 49152, false),
		Entry("with a script that encodes to over 64KiB, should be invalid", 49153, true),
	)

	DescribeTable("when detecting an enterprise config URL",
		func(input string, expected bool) {
			Expect(IsEnterpriseConfigURL(input)).To(Equal(expected))
		},
		Entry("with a GHES enterprise URL, should be an enterprise", "https://github.enterprise.com/enterprises/my-enterprise", true),
		Entry("with a github.com enterprise URL, should be an enterprise", "https://github.com/enterprises/my-enterprise", true),
		Entry("with a trailing slash, should be an enterprise", "https://github.com/enterprises/my-enterprise/", true),
		Entry("with mixed case, should be an enterprise", "https://github.com/Enterprises/my-enterprise", true),
		Entry("with an organization URL, should not be an enterprise", "https://github.com/my-org", false),
		Entry("with a repository URL, should not be an enterprise", "https://github.com/my-org/my-repo", false),
		Entry("with a repository named enterprises, should not be an enterprise", "https://github.com/my-org/enterprises", false),
		Entry("with an enterprise URL missing the name, should not be an enterprise", "https://github.com/enterprises", false),
		Entry("with an empty string, should not be an enterprise", "", false),
	)
})
