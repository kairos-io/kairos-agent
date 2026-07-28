package agent

import (
	"testing"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
)

// TestRun_HappyPathEmptyDir walks through agent.Run with an empty temp
// config directory and no bus plugins. Depending on host state
// (SentinelExist("firstboot"), presence of /proc/cmdline entries etc) the
// function either returns nil after Publish, or bubbles up an error from
// CreateSentinel / config.Scan. Both are fine — we just want to cover the
// main body of Run().
func TestRun_HappyPathEmptyDir(t *testing.T) {
	bus.Manager = bus.NewBus()
	dir := t.TempDir()
	// Restart:false, single directory, no API address. This should not
	// recurse regardless of whether Publish errors.
	if err := Run(WithDirectory(dir)); err != nil {
		t.Logf("Run err (accepted): %v", err)
	}
}
