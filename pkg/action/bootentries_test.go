package action

import (
	"bytes"
	"os"
	"syscall"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Bootentries tests", Label("bootentry"), func() {
	var config *sdkConfig.Config
	var fs vfs.FS
	var logger sdkLogger.KairosLogger
	var runner *v1mock.FakeRunner
	var mounter *v1mock.ErrorMounter
	var syscallMock *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var ghwTest ghwMock.GhwMock

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscallMock = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		logger.SetLevel("debug")
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		// Create proper dir structure for our EFI partition contentens
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/loader/entries", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/EFI/BOOT", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/EFI/kairos", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/etc/cos/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/run/initramfs/cos-state/grub/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/etc/kairos/branding/", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscallMock),
			agentConfig.WithClient(client),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
		)
		config.Collector = collector.Config{}

		mainDisk := sdkPartitions.Disk{
			Name: "device",
			Partitions: []*sdkPartitions.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					FS:              "ext4",
					MountPoint:      "/efi",
				},
			},
		}
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
		ghwTest.Clean()
		cleanup()
	})
	Context("Under Uki", func() {
		BeforeEach(func() {
			err := fs.Mkdir("/proc", os.ModeDir|os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/proc/cmdline", []byte("rd.immucore.uki"), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
		})
		Context("ListBootEntries", func() {
			It("fails to list the boot entries when there is no loader.conf", func() {
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
			})
		})
		Context("ListSystemdEntries", func() {
			It("lists the boot entries if there is any", func() {
				err := fs.WriteFile("/efi/loader/loader.conf", []byte("timeout 5\ndefault kairos\nrecovery kairos2\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive.conf", []byte("title kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery.conf", []byte("title kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset.conf", []byte("title kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listSystemdEntries(config, &sdkPartitions.Partition{MountPoint: "/efi"})
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(4))
				Expect(entries).To(ContainElement("cos"))
				Expect(entries).To(ContainElement("fallback"))
				Expect(entries).To(ContainElement("recovery"))
				Expect(entries).To(ContainElement("statereset"))

			})
			It("list empty boot entries if there is none", func() {
				entries, err := listSystemdEntries(config, &sdkPartitions.Partition{MountPoint: "/efi"})
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(0))
			})
		})
		Context("SelectBootEntry", func() {
			It("fails to select the boot entry if it doesnt exist", func() {
				err := SelectBootEntry(config, "kairos")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not exist"))
			})
			It("works without boot assessment", func() {
				err := fs.WriteFile("/efi/loader/entries/active.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte("default active.conf"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())
			})

			It("selects the boot entry in a default installation", func() {
				err := fs.WriteFile("/efi/loader/entries/active+2-1.conf", []byte("title kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive+3.conf", []byte("title kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery+1-2.conf", []byte("title kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset+2-1.conf", []byte("title kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "fallback")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("passive+3.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "recovery")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("recovery+1-2.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "statereset")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("statereset+2-1.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "cos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active+2-1.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				// also works using active (we want to get rid of the word cos later but this also needs to be applied in GRUB)
				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active+2-1.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())
			})

			It("selects the boot entry in a extend-cmdline installation with boot branding", func() {
				err := fs.WriteFile("/efi/loader/entries/active_install-mode_awesomeos.conf", []byte("title awesomeos\nefi /EFI/kairos/active_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive_install-mode_awesomeos+3.conf", []byte("title awesomeos (fallback)\nefi /EFI/kairos/passive_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery_install-mode_awesomeos.conf", []byte("title awesomeos recovery\nefi /EFI/kairos/recovery_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset_install-mode_awesomeos.conf", []byte("title awesomeos state reset (auto)\nefi /EFI/kairos/statereset_install-mode_awesomeos.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "fallback")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("passive_install-mode_awesomeos+3.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "recovery")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("recovery_install-mode_awesomeos.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "statereset")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("statereset_install-mode_awesomeos.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "cos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active_install-mode_awesomeos.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				// also works using active (we want to get rid of the word cos later but this also needs to be applied in GRUB)
				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active_install-mode_awesomeos.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())
			})

			It("selects the boot entry in a extra-cmdline installation", func() {
				err := fs.WriteFile("/efi/loader/entries/active.conf", []byte("title Kairos\nefi /EFI/kairos/active.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/active_foobar.conf", []byte("title Kairos\nefi /EFI/kairos/active_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive+3.conf", []byte("title Kairos (fallback)\nefi /EFI/kairos/passive.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/passive_foobar.conf", []byte("title Kairos (fallback)\nefi /EFI/kairos/passive_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery+3.conf", []byte("title Kairos recovery\nefi /EFI/kairos/recovery.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/recovery_foobar.conf", []byte("title Kairos recovery\nefi /EFI/kairos/recovery_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset.conf", []byte("title Kairos state reset (auto)\nefi /EFI/kairos/statereset.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/entries/statereset_foobar.conf", []byte("title Kairos state reset (auto)\nefi /EFI/kairos/state_reset_foobar.efi\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte("default active.conf"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = SelectBootEntry(config, "fallback")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("passive+3.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "fallback foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to fallback foobar"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("passive_foobar.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "recovery")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("recovery+3.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "recovery foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to recovery foobar"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("recovery_foobar.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "statereset")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("statereset.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "statereset foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to statereset foobar"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("statereset_foobar.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "cos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				err = SelectBootEntry(config, "cos foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to cos foobar"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active_foobar.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())

				// also works using active (we want to get rid of the word cos later but this also needs to be applied in GRUB)
				err = SelectBootEntry(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())
				err = SelectBootEntry(config, "active foobar")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to active foobar"))
				reader, err = utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("active_foobar.conf"))
				// Should have called a remount to make it RW
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT,
					"")).To(BeTrue())
				// Should have called a remount to make it RO
				Expect(syscallMock.WasMountCalledWith(
					"",
					"/efi",
					"",
					syscall.MS_REMOUNT|syscall.MS_RDONLY,
					"")).To(BeTrue())
			})
		})
	})

	Context("Under grub", func() {
		Context("ListBootEntries", func() {
			It("fails to list the boot entries when there is no grub files", func() {
				err := ListBootEntries(config)
				Expect(err).To(HaveOccurred())
			})
		})
		Context("ListSystemdEntries", func() {
			It("lists the boot entries if there is any", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/run/initramfs/cos-state/grub/grub.cfg", []byte("whatever whatever --id kairos2 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/etc/kairos/branding/grubmenu.cfg", []byte("whatever whatever --id kairos3 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listGrubEntries(config)
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(3))
				Expect(entries).To(ContainElement("kairos"))
				Expect(entries).To(ContainElement("kairos2"))
				Expect(entries).To(ContainElement("kairos3"))

			})
			It("list empty boot entries if there is none", func() {
				entries, err := listGrubEntries(config)
				Expect(err).To(HaveOccurred())
				Expect(entries).To(HaveLen(0))

			})
		})
		Context("SelectBootEntry", func() {
			BeforeEach(func() {
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					switch cmd {
					default:
						return []byte{}, nil
					}
				}
			})
			It("fails to select the boot entry if it doesnt exist", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/run/initramfs/cos-state/grub/grub.cfg", []byte("whatever whatever --id kairos2 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/etc/kairos/branding/grubmenu.cfg", []byte("whatever whatever --id kairos3 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err := SelectBootEntry(config, "nonexistant")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not exist"))
			})
			It("selects the boot entry", func() {
				err := fs.WriteFile("/etc/cos/grub.cfg", []byte("whatever whatever --id kairos {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/run/initramfs/cos-state/grub/grub.cfg", []byte("whatever whatever --id kairos2 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/etc/kairos/branding/grubmenu.cfg", []byte("whatever whatever --id kairos3 {"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				Expect(fs.Mkdir("/oem", os.ModePerm)).To(Succeed())

				err = SelectBootEntry(config, "kairos")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to kairos"))
				variables, err := utils.ReadPersistentVariables("/oem/grubenv", config)
				Expect(err).ToNot(HaveOccurred())
				Expect(variables["next_entry"]).To(Equal("kairos"))
			})
		})
	})
})
