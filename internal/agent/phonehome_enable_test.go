package agent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

func TestEnablePhoneHomeIfConfigured_NoPhonehomeSectionIsNoop(t *testing.T) {
	// With no phonehome key in the merged Collector output,
	// LoadFromCollector returns (nil, false, nil) and the function returns
	// without writing the systemd unit or invoking any command.
	logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
	cfg := config.NewConfig(config.WithLogger(logger))
	cfg.Collector = collector.Config{}
	enablePhoneHomeIfConfigured(cfg)
}

func TestEnablePhoneHomeIfConfigured_WithURLWritesUnit(t *testing.T) {
	// Redirect the service path to a temp file and stub exec.Command with a
	// harmless /bin/true equivalent so nothing runs systemctl on the host.
	dir := t.TempDir()
	unit := filepath.Join(dir, "kairos-agent-phonehome.service")

	origPath := phoneHomeServicePath
	origExec := phoneHomeExecCommand
	phoneHomeServicePath = unit
	phoneHomeExecCommand = func(_ string, _ ...string) *exec.Cmd {
		// echo is universally available and returns quickly with rc 0
		return exec.Command("/bin/sh", "-c", "true")
	}
	defer func() {
		phoneHomeServicePath = origPath
		phoneHomeExecCommand = origExec
	}()

	logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
	cfg := config.NewConfig(config.WithLogger(logger))
	cfg.Collector = collector.Config{
		Values: collector.ConfigValues{
			"phonehome": collector.ConfigValues{
				"url": "https://example.invalid/api",
			},
		},
	}
	enablePhoneHomeIfConfigured(cfg)

	// The unit file must have been written.
	b, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("expected unit file at %s: %v", unit, err)
	}
	if len(b) == 0 {
		t.Fatal("unit file is empty")
	}
}

func TestEnablePhoneHomeIfConfigured_WriteFailure(t *testing.T) {
	origPath := phoneHomeServicePath
	origExec := phoneHomeExecCommand
	// /proc/... is not writable → os.WriteFile returns an error and the
	// function early-returns after logging a warning. Exec is stubbed to a
	// no-op just in case.
	phoneHomeServicePath = "/proc/kairos-no-such-dir-xyz/service"
	phoneHomeExecCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("/bin/sh", "-c", "true")
	}
	defer func() {
		phoneHomeServicePath = origPath
		phoneHomeExecCommand = origExec
	}()

	logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
	cfg := config.NewConfig(config.WithLogger(logger))
	cfg.Collector = collector.Config{
		Values: collector.ConfigValues{
			"phonehome": collector.ConfigValues{"url": "https://example.invalid/api"},
		},
	}
	enablePhoneHomeIfConfigured(cfg)
}
