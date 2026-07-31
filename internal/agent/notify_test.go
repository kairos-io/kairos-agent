package agent_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
)

func writeCloudConfig(dir, contents string) {
	Expect(os.WriteFile(filepath.Join(dir, "cloud.yaml"), []byte(contents), 0o600)).To(Succeed(), "write config")
}

var _ = Describe("Notify", func() {
	// The undefined-event case covers the early-return error path in Notify
	// (before it publishes anything on the shared bus, which would risk running
	// stale plugin registrations from other tests in this package).
	It("errors on an undefined event", func() {
		dir := GinkgoT().TempDir()
		writeCloudConfig(dir, "#cloud-config\nfoo: bar\n")
		err := Notify("this-event-does-not-exist", []string{dir})
		Expect(err).To(HaveOccurred(), "expected error for undefined event")
		Expect(err.Error()).To(ContainSubstring("not defined"), "unexpected error: %v", err)
	})

	It("returns nil for a known event with no plugins", func() {
		// Reset the shared bus to avoid picking up any plugin registrations from
		// earlier tests (bus.go exits the process on plugin errors, which would
		// crash this test in the presence of leftover plugins).
		bus.Manager = bus.NewBus()
		dir := GinkgoT().TempDir()
		writeCloudConfig(dir, "#cloud-config\nusers:\n  - name: kairos\n    passwd: kairos\n")
		// Also pass an empty provider path override so LoadProviders does not
		// scan the working directory (which may contain leftover
		// agent-provider-* test artifacts).
		writeCloudConfig(dir, "#cloud-config\nproviders:\n  paths:\n  - "+GinkgoT().TempDir()+"\n")

		Expect(Notify("agent.install", []string{dir})).To(Succeed())
	})

	It("tolerates malformed provider paths", func() {
		// providers.paths as a scalar instead of a sequence → yaml.Unmarshal
		// errors and Notify logs a warning but continues to Initialize with the
		// default paths.
		bus.Manager = bus.NewBus()
		dir := GinkgoT().TempDir()
		writeCloudConfig(dir, "#cloud-config\nproviders:\n  paths: not-a-list\n")
		// event-level validation runs after providers parsing, so any known event
		// will succeed publishing (empty plugin set).
		Expect(Notify("agent.install", []string{dir})).To(Succeed())
	})
})
