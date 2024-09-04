/*
   Copyright © 2022 SUSE LLC

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package action_test

import (
	"bytes"
	"fmt"
	"github.com/diskfs/go-diskfs"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"os"
	"path/filepath"
	"regexp"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4"
	"github.com/twpayne/go-vfs/v4/vfst"
)

var _ = Describe("Install action tests", func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkTypes.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var cl *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var ghwTest v1mock.GhwMock
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		cl = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		logger.SetLevel("debug")
		var err error
		// create fake files needed for the loop device to "work"
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control": "",
			"/dev/loop0":        "",
		})
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(cl),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
		)
		config.Install = &agentConfig.Install{}
		config.Bundles = agentConfig.Bundles{}
		config.Config = collector.Config{}
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Install Action", Label("install"), func() {
		var device, cmdFail string
		var err error
		var spec *v1.InstallSpec
		var installer *action.InstallAction

		BeforeEach(func() {
			device = "/tmp/test.img"
			Expect(os.RemoveAll(device)).Should(Succeed())
			// at least 2Gb in size as state is set to 1G
			_, err = diskfs.Create(device, 2*1024*1024*1024, diskfs.Raw, 512)
			Expect(err).ToNot(HaveOccurred())

			config.Install.Device = device
			err = fsutils.MkdirAll(fs, filepath.Dir(device), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(device)
			Expect(err).ShouldNot(HaveOccurred())

			cmdFail = ""
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				regexCmd := regexp.MustCompile(cmdFail)
				if cmdFail != "" && regexCmd.MatchString(cmd) {
					return []byte{}, fmt.Errorf("failed on %s", cmd)
				}
				switch cmd {
				case "lsblk":
					return []byte(`{
"blockdevices":
    [
        {"label": "COS_ACTIVE", "type": "loop", "path": "/some/loop0"},
        {"label": "COS_OEM", "type": "part", "path": "/some/device1"},
        {"label": "COS_RECOVERY", "type": "part", "path": "/some/device2"},
        {"label": "COS_STATE", "type": "part", "path": "/some/device3"},
        {"label": "COS_PERSISTENT", "type": "part", "path": "/some/device4"}
    ]
}`), nil
				default:
					return []byte{}, nil
				}
			}
			// Need to create the IsoBaseTree, like if we are booting from iso
			err = fsutils.MkdirAll(fs, constants.IsoBaseTree, constants.DirPerm)
			Expect(err).To(BeNil())

			spec, err = agentConfig.NewInstallSpec(config)
			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Sanitize()).To(Succeed())
			spec.Active.Size = 16

			grubCfg := filepath.Join(spec.Active.MountPoint, constants.GrubConf)
			err = fsutils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(grubCfg)
			Expect(err).To(BeNil())

			mainDisk := block.Disk{
				Name: "device",
				Partitions: []*block.Partition{
					{
						Name:            "device1",
						FilesystemLabel: "COS_GRUB",
						Type:            "ext4",
					},
					{
						Name:            "device2",
						FilesystemLabel: "COS_STATE",
						Type:            "ext4",
					},
					{
						Name:            "device3",
						FilesystemLabel: "COS_PERSISTENT",
						Type:            "ext4",
					},
					{
						Name:            "device4",
						FilesystemLabel: "COS_ACTIVE",
						Type:            "ext4",
					},
					{
						Name:            "device5",
						FilesystemLabel: "COS_PASSIVE",
						Type:            "ext4",
					},
					{
						Name:            "device5",
						FilesystemLabel: "COS_RECOVERY",
						Type:            "ext4",
					},
					{
						Name:            "device6",
						FilesystemLabel: "COS_OEM",
						Type:            "ext4",
					},
				},
			}
			ghwTest = v1mock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			installer = action.NewInstallAction(config, spec)
		})
		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				GinkgoWriter.Printf(memLog.String())
			}
			Expect(os.RemoveAll(device)).ToNot(HaveOccurred())
			ghwTest.Clean()
		})

		It("Successfully installs", func() {
			spec.Target = device
			Expect(installer.Run()).To(BeNil())
		})

		It("Sets the executable /run/cos/ejectcd so systemd can eject the cd on restart", func() {
			_ = fsutils.MkdirAll(fs, "/usr/lib/systemd/system-shutdown", constants.DirPerm)
			_, err := fs.Stat("/usr/lib/systemd/system-shutdown/eject")
			Expect(err).To(HaveOccurred())
			// Override cmdline to return like we are booting from cd
			_ = fsutils.MkdirAll(fs, "/proc", constants.DirPerm)
			Expect(fs.WriteFile("/proc/cmdline", []byte("cdroot"), constants.FilePerm)).ToNot(HaveOccurred())

			spec.Target = device
			config.EjectCD = true
			Expect(installer.Run()).To(BeNil())
			_, err = fs.Stat("/usr/lib/systemd/system-shutdown/eject")
			Expect(err).ToNot(HaveOccurred())
			file, err := fs.ReadFile("/usr/lib/systemd/system-shutdown/eject")
			Expect(err).ToNot(HaveOccurred())
			Expect(file).To(ContainSubstring(constants.EjectScript))
		})

		It("ejectcd does nothing if we are not booting from cd", func() {
			_ = fsutils.MkdirAll(fs, "/usr/lib/systemd/system-shutdown", constants.DirPerm)
			_, err := fs.Stat("/usr/lib/systemd/system-shutdown/eject")
			Expect(err).To(HaveOccurred())
			spec.Target = device
			config.EjectCD = true
			Expect(installer.Run()).To(BeNil())
			_, err = fs.Stat("/usr/lib/systemd/system-shutdown/eject")
			Expect(err).To(HaveOccurred())
		})

		It("Successfully installs despite hooks failure", Label("hooks"), func() {
			cloudInit.Error = true
			spec.Target = device
			Expect(installer.Run()).To(BeNil())
		})

		It("Successfully installs without formatting despite detecting a previous installation", Label("no-format", "disk"), func() {
			spec.NoFormat = true
			spec.Force = true
			spec.Target = device
			Expect(installer.Run()).To(BeNil())
		})

		It("Successfully installs a docker image", Label("docker"), func() {
			spec.Target = device
			spec.Active.Source = v1.NewDockerSrc("my/image:latest")
			Expect(installer.Run()).To(BeNil())
		})

		It("Successfully installs and adds remote cloud-config", Label("cloud-config"), func() {
			spec.Target = device
			spec.CloudInit = []string{"http://my.config.org"}
			fsutils.MkdirAll(fs, constants.OEMDir, constants.DirPerm)
			_, err := fs.Create(filepath.Join(constants.OEMDir, "90_custom.yaml"))
			Expect(err).ShouldNot(HaveOccurred())
			Expect(installer.Run()).To(BeNil())
			Expect(cl.WasGetCalledWith("http://my.config.org")).To(BeTrue())
		})

		It("Fails if disk doesn't exist", Label("disk"), func() {
			spec.Target = "nonexistingdisk"
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails if some hook fails and strict is set", Label("strict"), func() {
			spec.Target = device
			config.Strict = true
			cloudInit.Error = true
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails to install from ISO if the ISO is not found", Label("iso"), func() {
			// Remove the ISO base tree so the mounted ISO is not found
			err = fs.RemoveAll(constants.IsoBaseTree)
			spec.Iso = "http://nonexistingiso"
			spec.Target = device
			cl.Error = true
			Expect(installer.Run()).NotTo(BeNil())
			Expect(cl.WasGetCalledWith("http://nonexistingiso")).To(BeTrue())
		})

		It("Fails to install from ISO as rsync can't find the temporary root tree", Label("iso"), func() {
			fs.Create("cOS.iso")
			spec.Iso = "http://cOS.iso"
			spec.Target = device
			err := installer.Run()
			Expect(err).To(BeNil())
			Expect(spec.Active.Source.Value()).To(ContainSubstring("/rootfs"))
			Expect(spec.Active.Source.IsDir()).To(BeTrue())
		})

		It("Fails to install without formatting if a previous install is detected", Label("no-format", "disk"), func() {
			spec.NoFormat = true
			spec.Force = false
			spec.Target = device
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails to mount partitions", Label("disk", "mount"), func() {
			spec.Target = device
			mounter.ErrorOnMount = true
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails on blkdeactivate errors", Label("disk", "partitions"), func() {
			spec.Target = device
			cmdFail = "blkdeactivate"
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails to unmount partitions", Label("disk", "partitions"), func() {
			spec.Target = device
			mounter.ErrorOnUnmount = true
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails to create a filesystem image", Label("disk", "image"), func() {
			spec.Target = device
			config.Fs = vfs.NewReadOnlyFS(fs)
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails if luet fails to unpack image", Label("image", "luet", "unpack"), func() {
			spec.Target = device
			extractor.SideEffect = func(imageRef, destination, platformRef string) error {
				return fmt.Errorf("error")
			}
			spec.Active.Source = v1.NewDockerSrc("my/image:latest")
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails if requested remote cloud config can't be downloaded", Label("cloud-config"), func() {
			spec.Target = device
			spec.CloudInit = []string{"http://my.config.org"}
			cl.Error = true
			Expect(installer.Run()).NotTo(BeNil())
			Expect(cl.WasGetCalledWith("http://my.config.org")).To(BeTrue())
		})

		It("Fails on grub2-install errors", Label("grub"), func() {
			spec.Target = device
			cmdFail = utils.FindCommand("grub2-install", []string{"grub2-install", "grub-install"})
			Expect(installer.Run()).NotTo(BeNil())
			Expect(runner.MatchMilestones([][]string{{"grub2-install"}}))
		})

		It("Fails copying Passive image", Label("copy", "active"), func() {
			spec.Target = device
			cmdFail = "tune2fs"
			Expect(installer.Run()).NotTo(BeNil())
			Expect(runner.MatchMilestones([][]string{{"tune2fs", "-L", constants.PassiveLabel}}))
		})
	})
})
