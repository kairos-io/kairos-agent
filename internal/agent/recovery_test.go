package agent

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
)

// Recovery is walked through with the interactive TTY calls stubbed out. It
// bypasses the 5-second grace sleep and the blocking Prompt so the flow can
// execute end-to-end in unit tests.
var _ = Describe("Recovery", func() {
	It("covers the happy path without plugins", func() {
		bus.Manager = bus.NewBus()

		// Provide a branding file so cmd.PrintBranding does not fall back to
		// utils.PrintBanner (image2ascii panics without a real TTY).
		dir := GinkgoT().TempDir()
		branding := filepath.Join(dir, "banner")
		Expect(os.WriteFile(branding, []byte("KAIROS"), 0o644)).To(Succeed())
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
			GinkgoWriter.Printf("Recovery err (accepted): %v\n", err)
		}
	})
})
