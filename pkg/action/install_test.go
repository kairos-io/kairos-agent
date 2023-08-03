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
	"errors"
	"fmt"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	"path/filepath"
	"regexp"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
)

const printOutput = `BYT;
/dev/loop0:50593792s:loopback:512:512:gpt:Loopback device:;`
const partTmpl = `
%d:%ss:%ss:2048s:ext4::type=83;`

var _ = Describe("Install action tests", func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger v1.Logger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var ghwTest v1mock.GhwMock
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = v1.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		//logger.SetLevel(v1.DebugLevel())
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(client),
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
		var cmdline func() ([]byte, error)
		var spec *v1.InstallSpec
		var installer *action.InstallAction

		BeforeEach(func() {
			device = "/some/device"
			config.Install.Device = device
			err = fsutils.MkdirAll(fs, filepath.Dir(device), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(device)
			Expect(err).ShouldNot(HaveOccurred())

			partNum := 0
			partedOut := printOutput
			cmdFail = ""
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				regexCmd := regexp.MustCompile(cmdFail)
				if cmdFail != "" && regexCmd.MatchString(cmd) {
					return []byte{}, errors.New(fmt.Sprintf("failed on %s", cmd))
				}
				switch cmd {
				case "parted":
					idx := 0
					for i, arg := range args {
						if arg == "mkpart" {
							idx = i
							break
						}
					}
					if idx > 0 {
						partNum++
						partedOut += fmt.Sprintf(partTmpl, partNum, args[idx+3], args[idx+4])
						_, _ = fs.Create(fmt.Sprintf("/some/device%d", partNum))
					}
					return []byte(partedOut), nil
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
				case "cat":
					if args[0] == "/proc/cmdline" {
						return cmdline()
					}
					return []byte{}, nil
				default:
					return []byte{}, nil
				}
			}
			// Need to create the IsoBaseTree, like if we are booting from iso
			err = fsutils.MkdirAll(fs, constants.IsoBaseTree, constants.DirPerm)
			Expect(err).To(BeNil())

			spec = agentConfig.NewInstallSpec(config)
			spec.Active.Size = 16

			grubCfg := filepath.Join(spec.Active.MountPoint, constants.GrubConf)
			err = fsutils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(grubCfg)
			Expect(err).To(BeNil())

			// Set default cmdline function so we dont panic :o
			cmdline = func() ([]byte, error) {
				return []byte{}, nil
			}
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
			cmdline = func() ([]byte, error) {
				return []byte("cdroot"), nil
			}
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
			Expect(client.WasGetCalledWith("http://my.config.org")).To(BeTrue())
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
			spec.Iso = "nonexistingiso"
			spec.Target = device
			Expect(installer.Run()).NotTo(BeNil())
		})

		It("Fails to install from ISO as rsync can't find the temporary root tree", Label("iso"), func() {
			fs.Create("cOS.iso")
			spec.Iso = "cOS.iso"
			spec.Target = device
			Expect(installer.Run()).NotTo(BeNil())
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
			Expect(runner.MatchMilestones([][]string{{"parted"}}))
		})

		It("Fails on parted errors", Label("disk", "partitions"), func() {
			spec.Target = device
			cmdFail = "parted"
			Expect(installer.Run()).NotTo(BeNil())
			Expect(runner.MatchMilestones([][]string{{"parted"}}))
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
			client.Error = true
			Expect(installer.Run()).NotTo(BeNil())
			Expect(client.WasGetCalledWith("http://my.config.org")).To(BeTrue())
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

		It("Fails setting the grub default entry", Label("grub"), func() {
			spec.Target = device
			spec.GrubDefEntry = "cOS"
			cmdFail = utils.FindCommand("grub2-editenv", []string{"grub2-editenv", "grub-editenv"})
			Expect(installer.Run()).NotTo(BeNil())
			Expect(runner.MatchMilestones([][]string{{cmdFail, filepath.Join(constants.StateDir, constants.GrubOEMEnv)}}))
		})
	})
})
