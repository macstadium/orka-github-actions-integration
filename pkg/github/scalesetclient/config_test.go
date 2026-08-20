package scalesetclient

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("config URL handling", func() {
	DescribeTable("accepted config URLs",
		func(configURL string) {
			_, err := newSDKClientForURL(configURL)
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("organization", "https://github.com/testorg"),
		Entry("repository", "https://github.com/testorg/testrepo"),
		Entry("GHES organization", "https://ghes.example.com/testorg"),
		Entry("GHE.com data residency", "https://acme.ghe.com/testorg"),
		Entry("enterprise", "https://github.com/enterprises/acme"),
	)

	DescribeTable("rejected config URLs",
		func(configURL string) {
			_, err := newSDKClientForURL(configURL)
			Expect(err).To(HaveOccurred())
		},
		Entry("no path", "https://github.com"),
		Entry("too many path segments", "https://github.com/a/b/c"),
	)
})
