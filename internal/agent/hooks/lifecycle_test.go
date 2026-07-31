package hook

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("gracePeriodMessage", func() {
	DescribeTable("builds the grace period message",
		func(action, expected string) {
			Expect(gracePeriodMessage(action)).To(Equal(expected))
		},
		Entry("power off message announces the grace period and how to cancel",
			"Powering off node", "Powering off node in 5s, press Ctrl+C to cancel"),
		Entry("reboot message announces the grace period and how to cancel",
			"Rebooting node", "Rebooting node in 5s, press Ctrl+C to cancel"),
	)
})
