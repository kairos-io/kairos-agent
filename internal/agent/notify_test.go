package agent_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
)

func writeCloudConfig(t *testing.T, dir, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "cloud.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestNotify_UndefinedEvent covers the early-return error path in Notify
// (before it publishes anything on the shared bus, which would risk running
// stale plugin registrations from other tests in this package).
func TestNotify_UndefinedEvent(t *testing.T) {
	dir := t.TempDir()
	writeCloudConfig(t, dir, "#cloud-config\nfoo: bar\n")
	err := Notify("this-event-does-not-exist", []string{dir})
	if err == nil {
		t.Fatal("expected error for undefined event")
	}
	if !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotify_KnownEventNoPluginsReturnsNil(t *testing.T) {
	// Reset the shared bus to avoid picking up any plugin registrations from
	// earlier tests (bus.go exits the process on plugin errors, which would
	// crash this test in the presence of leftover plugins).
	bus.Manager = bus.NewBus()
	dir := t.TempDir()
	writeCloudConfig(t, dir, "#cloud-config\nusers:\n  - name: kairos\n    passwd: kairos\n")
	// Also pass an empty provider path override so LoadProviders does not
	// scan the working directory (which may contain leftover
	// agent-provider-* test artifacts).
	writeCloudConfig(t, dir, "#cloud-config\nproviders:\n  paths:\n  - "+t.TempDir()+"\n")

	if err := Notify("agent.install", []string{dir}); err != nil {
		t.Fatalf("Notify err: %v", err)
	}
}

func TestNotify_MalformedProviderPaths(t *testing.T) {
	// providers.paths as a scalar instead of a sequence → yaml.Unmarshal
	// errors and Notify logs a warning but continues to Initialize with the
	// default paths.
	bus.Manager = bus.NewBus()
	dir := t.TempDir()
	writeCloudConfig(t, dir, "#cloud-config\nproviders:\n  paths: not-a-list\n")
	// event-level validation runs after providers parsing, so any known event
	// will succeed publishing (empty plugin set).
	if err := Notify("agent.install", []string{dir}); err != nil {
		t.Fatalf("Notify err: %v", err)
	}
}
