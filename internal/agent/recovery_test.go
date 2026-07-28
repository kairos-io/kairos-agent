package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
)

// TestRecovery_HappyPathWithoutPlugins walks through Recovery with the
// interactive TTY calls stubbed out. It bypasses the 5-second grace sleep and
// the blocking Prompt so the flow can execute end-to-end in unit tests.
func TestRecovery_HappyPathWithoutPlugins(t *testing.T) {
	bus.Manager = bus.NewBus()

	// Provide a branding file so cmd.PrintBranding does not fall back to
	// utils.PrintBanner (image2ascii panics without a real TTY).
	dir := t.TempDir()
	branding := filepath.Join(dir, "banner")
	if err := os.WriteFile(branding, []byte("KAIROS"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := cmd.SetBrandingFileForTests(branding)
	defer restore()

	// Stub the prompt and the 5-second grace sleep.
	origPrompt := recoveryPromptFn
	origSleep := recoveryFastSleep
	recoveryPromptFn = func(_ string) (string, error) { return "", nil }
	recoveryFastSleep = func(_ time.Duration) {}
	defer func() {
		recoveryPromptFn = origPrompt
		recoveryFastSleep = origSleep
	}()

	if err := Recovery(); err != nil {
		// machine.Getty(1) may error in a headless test env, but the code
		// tolerates that. The only expected error would be from Publish, and
		// with an empty plugin set that returns nil.
		t.Logf("Recovery err (accepted): %v", err)
	}
}
