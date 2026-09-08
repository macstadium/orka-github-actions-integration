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

	DescribeTable("when parsing a comma separated env",
		func(input string, expected []string) {
			Expect(parseCommaSeparated(input)).To(Equal(expected))
		},
		Entry("with a single name, should be one entry", "pixel8-api36", []string{"pixel8-api36"}),
		Entry("with several names, should keep order", "pixel8-api36,tablet-api35", []string{"pixel8-api36", "tablet-api35"}),
		Entry("with spaces around names, should trim them", " pixel8-api36 , tablet-api35 ", []string{"pixel8-api36", "tablet-api35"}),
		Entry("with a repeated name, should keep both entries", "pixel8-api36,pixel8-api36", []string{"pixel8-api36", "pixel8-api36"}),
		Entry("with an empty string, should be nil so the feature stays off", "", nil),
		Entry("with only whitespace, should be nil so the feature stays off", "   ", nil),
		Entry("with a trailing comma, should keep the empty entry for validation to reject", "pixel8-api36,", []string{"pixel8-api36", ""}),
		Entry("with an empty middle entry, should keep it for validation to reject", "a,,b", []string{"a", "", "b"}),
	)

	DescribeTable("when validating emulator configs",
		func(configs []string, timeout int, expectError bool) {
			envData := &Data{
				GitHubURL:                 "https://github.com/my-org",
				OrkaURL:                   "http://10.0.0.1",
				OrkaToken:                 "token",
				OrkaVMConfig:              "my-vm-config",
				OrkaEmulatorConfigs:       configs,
				OrkaEmulatorDeployTimeout: timeout,
			}

			errs := validateEnv(envData)

			if expectError {
				Expect(errs).ToNot(BeEmpty())
			} else {
				Expect(errs).To(BeEmpty())
			}
		},
		Entry("with no configs, should be valid and the feature off", nil, 10, false),
		Entry("with one config, should be valid", []string{"pixel8-api36"}, 10, false),
		Entry("with several configs, should be valid", []string{"pixel8-api36", "tablet-api35"}, 10, false),
		Entry("with an empty entry, should be invalid", []string{"pixel8-api36", ""}, 10, true),
		Entry("with a zero timeout while enabled, should be invalid", []string{"pixel8-api36"}, 0, true),
		Entry("with a negative timeout while enabled, should be invalid", []string{"pixel8-api36"}, -1, true),
		Entry("with a zero timeout while disabled, should be valid", nil, 0, false),
	)

	DescribeTable("when reporting whether emulators are enabled",
		func(configs []string, expected bool) {
			Expect((&Data{OrkaEmulatorConfigs: configs}).EmulatorsEnabled()).To(Equal(expected))
		},
		Entry("with no configs, should be disabled", nil, false),
		Entry("with an empty slice, should be disabled", []string{}, false),
		Entry("with one config, should be enabled", []string{"pixel8-api36"}, true),
	)

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
			GitHubURL:                 "https://github.com/my-org",
			OrkaURL:                   "http://10.0.0.1",
			OrkaToken:                 "token",
			OrkaVMConfig:              "my-vm-config",
			OrkaEmulatorDeployTimeout: 10,
		}
	}

	It("rejects setting both the config list and the inline list", func() {
		data := baseData()
		data.OrkaEmulatorConfigs = []string{"pixel8-api36"}
		data.OrkaEmulators = []EmulatorSpec{{Platform: "android-36", ImageType: "google_apis"}}

		Expect(validateEnv(data)).ToNot(BeEmpty())
	})

	It("accepts the inline list on its own", func() {
		data := baseData()
		data.OrkaEmulators = []EmulatorSpec{{Platform: "android-36", ImageType: "google_apis", DeviceProfile: "pixel_8"}}

		Expect(validateEnv(data)).To(BeEmpty())
	})

	DescribeTable("requires platform and image type on every inline entry",
		func(spec EmulatorSpec, valid bool) {
			data := baseData()
			data.OrkaEmulators = []EmulatorSpec{spec}

			if valid {
				Expect(validateEnv(data)).To(BeEmpty())
			} else {
				Expect(validateEnv(data)).ToNot(BeEmpty())
			}
		},
		Entry("with both set, should be valid", EmulatorSpec{Platform: "android-36", ImageType: "google_apis"}, true),
		Entry("with no platform, should be invalid", EmulatorSpec{ImageType: "google_apis"}, false),
		Entry("with no image type, should be invalid", EmulatorSpec{Platform: "android-36"}, false),
		Entry("with neither, should be invalid", EmulatorSpec{DeviceProfile: "pixel_8"}, false),
	)

	DescribeTable("counts emulators from whichever list is in use",
		func(configs []string, inline []EmulatorSpec, expected int, enabled bool) {
			data := &Data{OrkaEmulatorConfigs: configs, OrkaEmulators: inline}
			Expect(data.EmulatorCount()).To(Equal(expected))
			Expect(data.EmulatorsEnabled()).To(Equal(enabled))
		},
		Entry("with neither set", nil, nil, 0, false),
		Entry("with two named configs", []string{"a", "b"}, nil, 2, true),
		Entry("with one inline spec", nil, []EmulatorSpec{{Platform: "android-36", ImageType: "google_apis"}}, 1, true),
	)
})
