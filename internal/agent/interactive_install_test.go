package agent

import (
	"bytes"
	"strings"
	"testing"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

func TestInteractiveInstall_NoInstaller(t *testing.T) {
	// Ensure the env override is unset so Resolve falls back to disk paths.
	t.Setenv("KAIROS_INSTALLER", "")

	logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
	err := InteractiveInstall(false, "", logger)
	if err == nil {
		t.Skip("an installer is present on this host, cannot test the not-found branch")
	}
	if !strings.Contains(err.Error(), "no interactive installer found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
