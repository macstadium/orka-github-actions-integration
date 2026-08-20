package utils_test

import (
	"strconv"

	"github.com/macstadium/orka-github-actions-integration/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Utils tests", func() {
	Context("Map", func() {
		It("should apply the function to every element", func() {
			Expect(utils.Map([]int{1, 2, 3}, strconv.Itoa)).To(Equal([]string{"1", "2", "3"}))
		})

		It("should return an empty slice for an empty input", func() {
			Expect(utils.Map([]int{}, strconv.Itoa)).To(BeEmpty())
		})
	})
})
