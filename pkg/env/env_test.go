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
})
