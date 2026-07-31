package agent

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

var _ = Describe("enablePhoneHomeIfConfigured", func() {
	It("is a noop with no phonehome section", func() {
		// With no phonehome key in the merged Collector output,
		// LoadFromCollector returns (nil, false, nil) and the function returns
		// without writing the systemd unit or invoking any command.
		logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		cfg := config.NewConfig(config.WithLogger(logger))
		cfg.Collector = collector.Config{}
		enablePhoneHomeIfConfigured(cfg)
	})

	It("writes the unit when a URL is configured", func() {
		// Redirect the service path to a temp file and stub exec.Command with a
		// harmless /bin/true equivalent so nothing runs systemctl on the host.
		dir := GinkgoT().TempDir()
		unit := filepath.Join(dir, "kairos-agent-phonehome.service")

		origPath := phoneHomeServicePath
		origExec := phoneHomeExecCommand
		phoneHomeServicePath = unit
		phoneHomeExecCommand = func(_ string, _ ...string) *exec.Cmd {
			// echo is universally available and returns quickly with rc 0.
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
		Expect(err).ToNot(HaveOccurred(), "expected unit file at %s", unit)
		Expect(b).ToNot(BeEmpty(), "unit file is empty")
	})

	It("early-returns on write failure", func() {
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
	})
})
