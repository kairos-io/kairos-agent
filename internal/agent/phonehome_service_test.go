package agent

import (
	"github.com/kairos-io/kairos-sdk/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("phone-home service unit", func() {
	It("uses AgentDefaultPath in ExecStart instead of a hardcoded path", func() {
		unit := phoneHomeServiceUnit(constants.AgentDefaultPath)

		Expect(unit).To(ContainSubstring("ExecStart="+constants.AgentDefaultPath+" phone-home"), unit)
		Expect(unit).ToNot(ContainSubstring("/usr/sbin/kairos-agent"), unit)
	})
})
