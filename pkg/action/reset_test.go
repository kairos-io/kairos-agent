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
	"path/filepath"
	"regexp"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Reset action tests", func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkTypes.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var ghwTest ghwMock.GhwMock
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		var err error
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
			agentConfig.WithClient(client),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
		)
	})

	AfterEach(func() { cleanup() })

	Describe("Reset Action", Label("reset"), func() {
		var spec *v1.ResetSpec
		var reset *action.ResetAction
		var cmdFail, bootedFrom string
		var err error
		BeforeEach(func() {
			Expect(err).ShouldNot(HaveOccurred())
			cmdFail = ""
			recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
			err = fsutils.MkdirAll(fs, filepath.Dir(recoveryImg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(recoveryImg)
			Expect(err).To(BeNil())

			mainDisk := sdkTypes.Disk{
				Name: "device",
				Partitions: []*sdkTypes.Partition{
					{
						Name:            "device1",
						FilesystemLabel: "COS_GRUB",
						FS:              "ext4",
					},
					{
						Name:            "device2",
						FilesystemLabel: "COS_OEM",
						FS:              "ext4",
						MountPoint:      "/oem",
					},
					{
						Name:            "device3",
						FilesystemLabel: "COS_RECOVERY",
						FS:              "ext4",
						MountPoint:      "/run/initramfs/cos-state",
					},
					{
						Name:            "device4",
						FilesystemLabel: "COS_STATE",
						FS:              "ext4",
					},
					{
						Name:            "device5",
						FilesystemLabel: "COS_PERSISTENT",
						FS:              "ext4",
					},
				},
			}
			ghwTest = ghwMock.GhwMock{}
			ghwTest.AddDisk(mainDisk)
			ghwTest.CreateDevices()

			fs.Create(constants.EfiDevice)
			bootedFrom = constants.SystemLabel
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == cmdFail {
					return []byte{}, errors.New("Command failed")
				}
				switch cmd {
				case "cat":
					return []byte(bootedFrom), nil
				default:
					return []byte{}, nil
				}
			}

			spec, err = agentConfig.NewResetSpec(config)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(spec.Active.Source.IsEmpty()).To(BeFalse())

			spec.Active.Size = 16

			grubCfg := filepath.Join(spec.Active.MountPoint, spec.GrubConf)
			err = fsutils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(grubCfg)
			Expect(err).To(BeNil())

			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				regexCmd := regexp.MustCompile(cmdFail)
				if cmdFail != "" && regexCmd.MatchString(cmd) {
					return []byte{}, errors.New("command failed")
				}
				return []byte{}, nil
			}
			reset = action.NewResetAction(config, spec)
		})

		AfterEach(func() {
			ghwTest.Clean()
		})

		It("Successfully resets on non-squashfs recovery", func() {
			Expect(reset.Run()).To(BeNil())
		})
		It("Successfully resets on non-squashfs recovery including persistent data", func() {
			spec.FormatPersistent = true
			spec.FormatOEM = true
			Expect(reset.Run()).To(BeNil())
		})
		It("Successfully resets from a squashfs recovery image", Label("channel"), func() {
			err := fsutils.MkdirAll(config.Fs, constants.IsoBaseTree, constants.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			spec.Active.Source = v1.NewDirSrc(constants.IsoBaseTree)
			Expect(reset.Run()).To(BeNil())
		})
		It("Successfully resets despite having errors on hooks", func() {
			cloudInit.Error = true
			Expect(reset.Run()).To(BeNil())
		})
		It("Successfully resets from a docker image", Label("docker"), func() {
			spec.Active.Source = v1.NewDockerSrc("my/image:latest")
			Expect(reset.Run()).To(BeNil())

		})
		It("Fails installing grub", func() {
			cmdFail = utils.FindCommand("grub2-install", []string{"grub2-install", "grub-install"})
			Expect(reset.Run()).NotTo(BeNil())
			Expect(runner.IncludesCmds([][]string{{cmdFail}}))
		})
		It("Fails formatting state partition", func() {
			cmdFail = "mkfs.ext4"
			Expect(reset.Run()).NotTo(BeNil())
			Expect(runner.IncludesCmds([][]string{{"mkfs.ext4"}}))
		})
		It("Fails setting the active label on non-squashfs recovery", func() {
			cmdFail = "tune2fs"
			Expect(reset.Run()).NotTo(BeNil())
		})
		It("Fails setting the passive label on squashfs recovery", func() {
			cmdFail = "tune2fs"
			Expect(reset.Run()).NotTo(BeNil())
			Expect(runner.IncludesCmds([][]string{{"tune2fs"}}))
		})
		It("Fails mounting partitions", func() {
			mounter.ErrorOnMount = true
			Expect(reset.Run()).NotTo(BeNil())
		})
		It("Fails unmounting partitions", func() {
			mounter.ErrorOnUnmount = true
			Expect(reset.Run()).NotTo(BeNil())
		})
		It("Fails unpacking docker image ", func() {
			spec.Active.Source = v1.NewDockerSrc("my/image:latest")
			extractor.SideEffect = func(imageRef, destination, platformRef string) error {
				return fmt.Errorf("error")
			}
			Expect(reset.Run()).NotTo(BeNil())
		})
	})
})
