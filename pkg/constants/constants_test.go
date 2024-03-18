package constants_test

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Replace title", func() {
	DescribeTable("Replacing the tile",
		func(role, oldTitle, newTitle string) {
			Expect(constants.BootTitleForRole(role, oldTitle)).To(Equal(newTitle))
		},
		Entry("When seeting to active with a default title", "active", "My awesome OS", "My awesome OS"),
		Entry("When setting to active with a fallback title", "active", "My awesome OS (fallback)", "My awesome OS"),
		Entry("When setting to active with a recovery title", "active", "My awesome OS recovery", "My awesome OS"),
		Entry("When setting to passive with a default title", "passive", "My awesome OS", "My awesome OS (fallback)"),
		Entry("When setting to passive with a fallback title", "passive", "My awesome OS (fallback)", "My awesome OS (fallback)"),
		Entry("When setting to passive with a recovery title", "passive", "My awesome OS recovery", "My awesome OS (fallback)"),
		Entry("When setting to recovery with a default title", "recovery", "My awesome OS", "My awesome OS recovery"),
		Entry("When setting to recovery with a fallback title", "recovery", "My awesome OS (fallback)", "My awesome OS recovery"),
		Entry("When setting to recovery with a recovery title", "recovery", "My awesome OS recovery", "My awesome OS recovery"),
	)
})
