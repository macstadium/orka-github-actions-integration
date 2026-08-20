package env

import (
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
