package phonehome

import (
	"errors"
	"os"
	"path/filepath"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/mount-utils"
)

var _ = Describe("artifact upgrade download", func() {
	originalPersistentDir := persistentDir

	AfterEach(func() {
		persistentDir = originalPersistentDir
	})

	It("creates the temporary image on the mounted persistent partition", func() {
		persistentDir = GinkgoT().TempDir()
		config := &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
			{Device: "/dev/persistent", Path: persistentDir},
		})}

		file, err := createArtifactTempFile(config, "artifact-123")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(file.Name()) }()
		defer func() { _ = file.Close() }()

		Expect(filepath.Dir(file.Name())).To(Equal(filepath.Join(persistentDir, "tmp")))
		Expect(filepath.Base(file.Name())).To(HavePrefix("phonehome-upgrade-artifact-123-"))
		Expect(filepath.Ext(file.Name())).To(Equal(".tar"))
	})

	It("refuses to download when the persistent partition is not mounted", func() {
		persistentDir = GinkgoT().TempDir()
		config := &sdkConfig.Config{Mounter: mount.NewFakeMounter(nil)}

		_, err := createArtifactTempFile(config, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("is not mounted")))
	})

	It("refuses to download when the persistent partition mount cannot be verified", func() {
		_, err := createArtifactTempFile(nil, "artifact-123")

		Expect(err).To(MatchError("cannot verify the persistent partition mount"))
	})

	It("reports an error when the persistent temporary directory cannot be created", func() {
		tempDir := GinkgoT().TempDir()
		persistentDir = filepath.Join(tempDir, "persistent")
		Expect(os.WriteFile(persistentDir, []byte("not a directory"), 0600)).To(Succeed())
		config := &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
			{Device: "/dev/persistent", Path: persistentDir},
		})}

		_, err := createArtifactTempFile(config, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("creating persistent temporary directory")))
	})
})

var _ = Describe("handleReset", func() {
	var (
		originalSelectBootEntry = selectBootEntry
		originalRebootScheduler = rebootScheduler
	)

	AfterEach(func() {
		selectBootEntry = originalSelectBootEntry
		rebootScheduler = originalRebootScheduler
	})

	It("fails without the scanned system configuration", func() {
		_, err := handleReset(CommandData{}, nil)

		Expect(err).To(MatchError(ContainSubstring("scanned system configuration")))
	})

	It("selects the automatic state-reset entry before scheduling a reboot", func() {
		cfg := &sdkConfig.Config{}
		selected := ""
		rebootScheduled := false
		selectBootEntry = func(actualConfig *sdkConfig.Config, entry string) error {
			Expect(actualConfig).To(BeIdenticalTo(cfg))
			selected = entry
			return nil
		}
		rebootScheduler = func() { rebootScheduled = true }

		message, err := handleReset(CommandData{}, cfg)

		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal("statereset"))
		Expect(rebootScheduled).To(BeTrue())
		Expect(message).To(ContainSubstring("Rebooting"))
	})

	It("is dispatched by the default command handler", func() {
		cfg := &sdkConfig.Config{}
		selectBootEntry = func(*sdkConfig.Config, string) error { return nil }
		rebootScheduler = func() {}
		handler := DefaultCommandHandler("http://example", func() string { return "" }, func(command string) bool {
			return command == "reset"
		}, nil, cfg)

		message, err := handler(CommandData{Command: "reset"})

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(ContainSubstring("Automatic state reset selected"))
	})

	DescribeTable("rejects unsupported legacy arguments without scheduling a reset",
		func(argument string) {
			selectBootEntry = func(*sdkConfig.Config, string) error {
				Fail("boot entry must not be selected")
				return nil
			}
			rebootScheduler = func() { Fail("reboot must not be scheduled") }
			handler := DefaultCommandHandler("http://example", func() string { return "" }, func(string) bool {
				return true
			}, nil, &sdkConfig.Config{})

			_, err := handler(CommandData{Command: "reset", Args: map[string]string{argument: "value"}})

			Expect(err).To(MatchError(ContainSubstring("is not supported")))
		},
		Entry("reset-oem", "reset-oem"),
		Entry("config", "config"),
	)

	It("does not reboot when selecting the state-reset entry fails", func() {
		selectBootEntry = func(*sdkConfig.Config, string) error { return errors.New("selection failed") }
		rebootScheduler = func() { Fail("reboot must not be scheduled") }

		_, err := handleReset(CommandData{}, &sdkConfig.Config{})

		Expect(err).To(MatchError(ContainSubstring("selection failed")))
	})
})
