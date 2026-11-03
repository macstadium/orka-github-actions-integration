package env

import (
	"testing"
	"time"

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

var _ = Describe("Emulator env", func() {
	baseData := func() *Data {
		return &Data{
			GitHubURL:                     "https://github.com/my-org",
			OrkaURL:                       "http://10.0.0.1",
			OrkaToken:                     "token",
			OrkaVMConfig:                  "my-vm-config",
			OrkaEmulatorDeployTimeout:     10,
			EmulatorConfigRefreshInterval: 60 * time.Second,
		}
	}

	It("accepts the defaults", func() {
		Expect(validateEnv(baseData())).To(BeEmpty())
	})

	DescribeTable("requires a positive deploy timeout",
		func(timeout int, valid bool) {
			data := baseData()
			data.OrkaEmulatorDeployTimeout = timeout

			if valid {
				Expect(validateEnv(data)).To(BeEmpty())
			} else {
				Expect(validateEnv(data)).ToNot(BeEmpty())
			}
		},
		Entry("with a positive timeout, should be valid", 10, true),
		Entry("with a zero timeout, should be invalid", 0, false),
		Entry("with a negative timeout, should be invalid", -1, false),
	)

	DescribeTable("requires a positive refresh interval",
		func(interval time.Duration, valid bool) {
			data := baseData()
			data.EmulatorConfigRefreshInterval = interval

			if valid {
				Expect(validateEnv(data)).To(BeEmpty())
			} else {
				Expect(validateEnv(data)).ToNot(BeEmpty())
			}
		},
		Entry("with a positive interval, should be valid", 30*time.Second, true),
		Entry("with a zero interval, should be invalid", time.Duration(0), false),
		Entry("with a negative interval, should be invalid", -time.Second, false),
	)

	DescribeTable("flags a deploy timeout that cannot outlast the operator's unschedulable timeout",
		func(timeout int, tooShort bool) {
			data := baseData()
			data.OrkaEmulatorDeployTimeout = timeout
			Expect(data.EmulatorDeployTimeoutTooShort()).To(Equal(tooShort))
		},
		Entry("with the default of 10 minutes, should be fine", 10, false),
		Entry("with 6 minutes, should be fine", 6, false),
		Entry("with exactly 5 minutes, should race the operator", 5, true),
		Entry("with 3 minutes, should be too short", 3, true),
	)
})
