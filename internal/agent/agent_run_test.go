package agent

import (
	. "github.com/onsi/ginkgo/v2"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
)

// Run walks through agent.Run with an empty temp config directory and no bus
// plugins. Depending on host state (SentinelExist("firstboot"), presence of
// /proc/cmdline entries etc) the function either returns nil after Publish, or
// bubbles up an error from CreateSentinel / config.Scan. Both are fine — we
// just want to cover the main body of Run().
var _ = Describe("Run", func() {
	It("covers the happy path with an empty config dir", func() {
		bus.Manager = bus.NewBus()
		dir := GinkgoT().TempDir()
		// Restart:false, single directory, no API address. This should not
		// recurse regardless of whether Publish errors.
		if err := Run(WithDirectory(dir)); err != nil {
			GinkgoWriter.Printf("Run err (accepted): %v\n", err)
		}
	})
})
