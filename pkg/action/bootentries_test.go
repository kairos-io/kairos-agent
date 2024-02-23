package action

import (
	"bytes"
	"github.com/jaypipes/ghw/pkg/block"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
	"os"
	"syscall"
)

var _ = Describe("Bootentries tests", Label("bootentry"), func() {
	var config *agentConfig.Config
	var fs vfs.FS
	var logger v1.Logger
	var runner *v1mock.FakeRunner
	var mounter *v1mock.ErrorMounter
	var syscallMock *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var ghwTest v1mock.GhwMock

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscallMock = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = v1.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		logger.SetLevel(v1.DebugLevel())
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		// Create proper dir structure for our EFI partition contentens
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/boot/loader/entries", os.ModeDir|os.ModePerm)
		err = fsutils.MkdirAll(fs, "/efi/loader", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/EFI/BOOT", os.ModeDir|os.ModePerm)
		Expect(err).Should(BeNil())
		err = fsutils.MkdirAll(fs, "/efi/boot/EFI/kairos", os.ModeDir|os.ModePerm)
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
		config.Config = collector.Config{}

		mainDisk := block.Disk{
			Name: "device",
			Partitions: []*block.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					Type:            "ext4",
					MountPoint:      "/efi",
				},
				{
					Name:            "device2",
					FilesystemLabel: "COS_XBOOTLOADER",
					Type:            "ext4",
					MountPoint:      "/efi/boot",
				},
			},
		}
		ghwTest = v1mock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
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
				err = fs.WriteFile("/efi/boot/loader/entries/kairos.conf", []byte("title kairos\nlinux /vmlinuz\ninitrd /initrd\noptions root=LABEL=COS_GRUB\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = fs.WriteFile("/efi/boot/loader/entries/kairos2.conf", []byte("title kairos2\nlinux /vmlinuz2\ninitrd /initrd2\noptions root=LABEL=COS_GRUB2\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				entries, err := listSystemdEntries(config, &v1.Partition{MountPoint: "/efi/boot"})
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(2))
				Expect(entries).To(ContainElement("kairos.conf"))
				Expect(entries).To(ContainElement("kairos2.conf"))

			})
			It("list empty boot entries if there is none", func() {
				entries, err := listSystemdEntries(config, &v1.Partition{MountPoint: "/efi/boot"})
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
			It("selects the boot entry", func() {
				err := fs.WriteFile("/efi/boot/loader/entries/kairos.conf", []byte("title kairos\nlinux /vmlinuz\ninitrd /initrd\noptions root=LABEL=COS_GRUB\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/boot/loader/entries/kairos2.conf", []byte("title kairos\nlinux /vmlinuz\ninitrd /initrd\noptions root=LABEL=COS_GRUB\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)

				err = SelectBootEntry(config, "kairos.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to kairos"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("kairos.conf"))
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
			It("selects the boot entry with the missing .conf extension", func() {
				err := fs.WriteFile("/efi/boot/loader/entries/kairos.conf", []byte("title kairos\nlinux /vmlinuz\ninitrd /initrd\noptions root=LABEL=COS_GRUB\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/boot/loader/entries/kairos2.conf", []byte("title kairos\nlinux /vmlinuz\ninitrd /initrd\noptions root=LABEL=COS_GRUB\n"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = fs.WriteFile("/efi/loader/loader.conf", []byte(""), os.ModePerm)

				err = SelectBootEntry(config, "kairos2")
				Expect(err).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to kairos2"))
				reader, err := utils.SystemdBootConfReader(fs, "/efi/loader/loader.conf")
				Expect(err).ToNot(HaveOccurred())
				Expect(reader["default"]).To(Equal("kairos2.conf"))

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
				Expect(err).ToNot(HaveOccurred())
				Expect(entries).To(HaveLen(0))

			})
		})
		Context("SelectBootEntry", func() {
			BeforeEach(func() {
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					switch cmd {
					case "grub2-editenv":
						return []byte(""), nil
					default:
						return []byte{}, nil
					}
				}
			})
			It("fails to select the boot entry if it doesnt exist", func() {
				err := SelectBootEntry(config, "kairos")
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

				err = SelectBootEntry(config, "kairos")
				Expect(err).ToNot(HaveOccurred())
				Expect(runner.IncludesCmds([][]string{
					{"grub2-editenv", "/oem/grubenv", "set", "next_entry=kairos"},
				})).ToNot(HaveOccurred())
				Expect(memLog.String()).To(ContainSubstring("Default boot entry set to kairos"))
			})
		})
	})
})
