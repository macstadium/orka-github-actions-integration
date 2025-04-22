package utils_test

import (
	"time"

	"github.com/macstadium/orka-github-actions-integration/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Utils tests", func() {
	Context("GetTokenExpirationTime", func() {
		It("should correctly extract expiration time", func() {
			token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE2NTA3MDI3NDUsInVzZXJuYW1lIjoiYWRtaW4ifQ.s56k7k89uI3jJ245jB8kF1234567890"
			expiration, err := utils.GetTokenExpirationTime(token)

			Expect(err).To(BeNil())
			Expect(expiration).Should(BeTemporally("<", time.Now()))
		})

		It("should return error for a token with missing expiration claim", func() {
			token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImFkbWluIn0.s56k7k89uI3jJ245jB8kF1234567890"

			_, err := utils.GetTokenExpirationTime(token)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("missing expiration claim in token"))
		})
		It("should return error for invalid token", func() {
			token := "invalid-token"

			_, err := utils.GetTokenExpirationTime(token)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("failed to parse jwt token: token contains an invalid number of segments"))
		})
	})
})
