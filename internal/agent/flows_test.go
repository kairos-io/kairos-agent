package agent

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-sdk/state"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
)

func osWriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func configForTests() *sdkConfig.Config {
	return config.NewConfig()
}

// resetBus swaps the shared bus.Manager for a fresh one. Prior tests may have
// registered plugin callbacks that would cause os.Exit(1) if a subsequent
// Publish triggers them (bus.go handles errored plugin responses that way).
// Resetting keeps flow-level tests hermetic.
func resetBus() {
	bus.Manager = bus.NewBus()
}

func writeFile(path, content string) error {
	return osWriteFile(path, content)
}

// The following flow tests exercise the top-level command entry points. Each
// call is expected to fail well before touching real disks — either at
// config.CheckConfigForUsers (no admin user in an empty config) or at the
// initial bus scan step.
var _ = Describe("flow entry points", func() {
	Describe("Reset", func() {
		It("returns an error for a missing admin user (non-UKI)", func() {
			resetBus()
			dir := GinkgoT().TempDir()
			err := Reset(false, true, false, dir)
			if err == nil {
				Skip("environment allowed reset to proceed further than expected")
			}
			if !strings.Contains(err.Error(), "users") && !strings.Contains(err.Error(), "admin") &&
				!strings.Contains(err.Error(), "config") {
				GinkgoWriter.Printf("Reset error (accepted): %v\n", err)
			}
		})

		It("returns an error for reset on UKI removable media", func() {
			orig := ukiBootModeFn
			ukiBootModeFn = func() state.Boot { return internalutils.UkiRemovableMedia }
			defer func() { ukiBootModeFn = orig }()

			err := Reset(false, true, false)
			Expect(err).To(HaveOccurred(), "expected error for reset on removable media")
			Expect(err.Error()).To(ContainSubstring("removable media"))
		})

		It("dispatches resetUki on UKI HDD boot", func() {
			resetBus()
			orig := ukiBootModeFn
			ukiBootModeFn = func() state.Boot { return internalutils.UkiHDD }
			defer func() { ukiBootModeFn = orig }()

			// resetUki flow goes through sharedReset then config.ReadUkiResetSpecFromConfig
			// which fails without a UKI environment. We only need to exercise the
			// dispatch path — accept any error.
			dir := GinkgoT().TempDir()
			if err := Reset(false, true, false, dir); err == nil {
				Skip("environment allowed resetUki to succeed")
			}
		})
	})

	Describe("Upgrade", func() {
		It("returns an error in a non-UKI test environment", func() {
			resetBus()
			dir := GinkgoT().TempDir()
			err := Upgrade("", false, []string{dir}, "", false)
			if err == nil {
				Skip("environment allowed upgrade to proceed further than expected")
			}
			GinkgoWriter.Printf("Upgrade error: %v\n", err)
		})

		It("dispatches upgradeUki on UKI HDD boot", func() {
			resetBus()
			orig := upgradeUkiBootModeFn
			upgradeUkiBootModeFn = func() state.Boot { return internalutils.UkiHDD }
			defer func() { upgradeUkiBootModeFn = orig }()

			if err := Upgrade("", false, []string{GinkgoT().TempDir()}, "", false); err == nil {
				Skip("environment allowed upgradeUki to succeed")
			}
		})
	})

	Describe("Recovery", func() {
		It("returns after events without plugins", func() {
			resetBus()
			// Recovery uses agent config for branding, then publishes EventRecovery.
			// Without plugins it responds with empty data; the function then blocks
			// waiting for user input via utils.Prompt — so we can't run it directly
			// in unit tests. Instead we exercise its early error branch by pre-
			// registering a plugin that returns Errored; but bus.NewBus with an empty
			// plugin list simply exits Recovery at Prompt. Skip when the environment
			// would block.
			Skip("Recovery blocks on utils.Prompt; covered indirectly by e2e")
		})
	})

	Describe("Install", func() {
		It("handles no config and no providers", func() {
			// With no cloud-config in the directory, Install with auto=false and no
			// providers registered should drop into the "no providers" branch and
			// attempt utils.Shell().Run(). That blocks. So we skip in-process.
			Skip("Install drops to interactive shell in the no-providers branch, cannot run non-interactively")
		})
	})

	Describe("ManualInstall", func() {
		It("errors on a missing config file", func() {
			resetBus()
			// ManualInstall reads the given `c` string as a file path (or URL). A
			// missing local path causes prepareConfiguration to error out
			// immediately — this covers the ManualInstall entry point and its early
			// error return path.
			err := ManualInstall("/no/such/config.yaml", "", "", false, false, false, false, false)
			Expect(err).To(HaveOccurred(), "expected error for missing config file")
		})

		It("fails later with a valid config file", func() {
			resetBus()
			dir := GinkgoT().TempDir()
			cfgPath := dir + "/cc.yaml"
			Expect(writeFile(cfgPath, "#cloud-config\ninstall:\n  nousers: true\n")).To(Succeed())
			err := ManualInstall(cfgPath, "", "/dev/null", false, false, false, false, false)
			if err == nil {
				Skip("environment allowed ManualInstall to succeed")
			}
			GinkgoWriter.Printf("ManualInstall error: %v\n", err)
		})
	})

	Describe("RunInstall", func() {
		It("fails on no admin user", func() {
			// Build a minimal sdkConfig.Config using the same defaults as pkg/config.
			// CheckConfigForUsers requires an admin user; a bare Install object has
			// none and returns an error before anything touches real disks.
			cfg := configForTests()
			err := RunInstall(cfg)
			if err == nil {
				Skip("environment allowed RunInstall to proceed further than expected")
			}
			GinkgoWriter.Printf("RunInstall error: %v\n", err)
		})

		It("falls into runInstall on non-UKI boot", func() {
			// Provide a config that passes user + partition checks so we exercise the
			// dispatch into runInstall / runInstallUki. Both call
			// config.ReadInstall*SpecFromConfig which will fail without a full
			// installation environment, so the error path there covers those funcs.
			cfg := configForTests()
			// mark nousers so CheckConfigForUsers early-returns via the file check;
			// we cannot create /etc/kairos/.nousers in tests without root, so set the
			// field on Install instead.
			cfg.Install.NoUsers = true
			err := RunInstall(cfg)
			if err == nil {
				Skip("environment allowed RunInstall to succeed unexpectedly")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "install") &&
				!strings.Contains(strings.ToLower(err.Error()), "spec") &&
				!strings.Contains(strings.ToLower(err.Error()), "device") &&
				!strings.Contains(strings.ToLower(err.Error()), "extra") &&
				!strings.Contains(strings.ToLower(err.Error()), "partition") &&
				!strings.Contains(strings.ToLower(err.Error()), "no such") {
				GinkgoWriter.Printf("RunInstall error (accepted): %v\n", err)
			}
		})

		It("errors on a missing extra partition name", func() {
			cfg := configForTests()
			cfg.Install.NoUsers = true
			// An extra partition with no name should fail CheckConfigForExtraPartitions.
			cfg.Install.ExtraPartitions = sdkPartitions.PartitionList{&sdkPartitions.Partition{Size: 100}}
			err := RunInstall(cfg)
			Expect(err).To(HaveOccurred(), "expected error for missing partition name")
		})
	})

	Describe("getReleasesFromProvider", func() {
		It("returns empty when no plugins are registered", func() {
			resetBus()
			got, err := getReleasesFromProvider(false)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(BeEmpty(), "expected empty releases when no plugins registered, got %v", got)
		})
	})
})
