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
	"github.com/kairos-io/kairos/v2/pkg/action"
	"github.com/kairos-io/kairos/v2/pkg/constants"
	conf "github.com/kairos-io/kairos/v2/pkg/elementalConfig"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	"github.com/kairos-io/kairos/v2/pkg/utils"
	v1mock "github.com/kairos-io/kairos/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
	"os"
	"path/filepath"
)

var _ = Describe("Reset action tests", func() {
	var config *v1.RunConfig
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger v1.Logger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = v1.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = conf.NewRunConfig(
			conf.WithFs(fs),
			conf.WithRunner(runner),
			conf.WithLogger(logger),
			conf.WithMounter(mounter),
			conf.WithSyscall(syscall),
			conf.WithClient(client),
			conf.WithCloudInitRunner(cloudInit),
			conf.WithImageExtractor(extractor),
		)
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Reset Action", Label("reset"), func() {
		var spec *v1.ResetSpec
		var reset *action.ResetAction
		var cmdFail, bootedFrom string
		var err error

		BeforeEach(func() {
			cmdFail = ""
			recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
			err = utils.MkdirAll(fs, filepath.Dir(recoveryImg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(recoveryImg)
			Expect(err).To(BeNil())

			err := fs.Mkdir("/dev", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.Mkdir("/dev/disk", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.Mkdir("/dev/disk/by-path", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/dev/disk/by-path/device1", []byte{}, os.ModePerm)
			err = fs.WriteFile("/dev/disk/by-path/device2", []byte{}, os.ModePerm)
			err = fs.WriteFile("/dev/disk/by-path/device3", []byte{}, os.ModePerm)
			err = fs.WriteFile("/dev/disk/by-path/device4", []byte{}, os.ModePerm)
			err = fs.WriteFile("/dev/disk/by-path/device5", []byte{}, os.ModePerm)

			fs.Create(constants.EfiDevice)
			bootedFrom = constants.SystemLabel
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == cmdFail {
					return []byte{}, errors.New("Command failed")
				}
				switch cmd {
				case "cat":
					return []byte(bootedFrom), nil
				case "lsblk":
					if args[0] == "--list" && args[2] == "/dev/disk/by-path/device1" {
						return []byte(`{
							"blockdevices": [
									{
										"name": "device1",
										"pkname": null,
										"path": "/dev/device1",
										"fstype": "ext4",
										"mountpoint": null,
										"size": 64088965120,
										"ro": false,
										"label": "COS_STATE"
									}
							]}`), nil
					}
					if args[0] == "--list" && args[2] == "/dev/disk/by-path/device2" {
						return []byte(`{
							"blockdevices": [
									{
										"name": "device2",
										"pkname": null,
										"path": "/dev/device2",
										"fstype": "ext4",
										"mountpoint": null,
										"size": 64088965120,
										"ro": false,
										"label": "COS_RECOVERY"
									}
							]}`), nil
					}
					if args[0] == "--list" && args[2] == "/dev/disk/by-path/device3" {
						return []byte(`{
							"blockdevices": [
									{
										"name": "device3",
										"pkname": null,
										"path": "/dev/device3",
										"fstype": "ext4",
										"mountpoint": null,
										"size": 64088965120,
										"ro": false,
										"label": "COS_ACTIVE"
									}
							]}`), nil
					}
					if args[0] == "--list" && args[2] == "/dev/disk/by-path/device4" {
						return []byte(`{
							"blockdevices": [
									{
										"name": "device4",
										"pkname": null,
										"path": "/dev/device4",
										"fstype": "ext4",
										"mountpoint": null,
										"size": 64088965120,
										"ro": false,
										"label": "COS_OEM"
									}
							]}`), nil
					}
					if args[0] == "--list" && args[2] == "/dev/disk/by-path/device5" {
						return []byte(`{
							"blockdevices": [
									{
										"name": "device5",
										"pkname": null,
										"path": "/dev/device5",
										"fstype": "ext4",
										"mountpoint": null,
										"size": 64088965120,
										"ro": false,
										"label": "COS_PERSISTENT"
									}
							]}`), nil
					}
				}

				return []byte{}, nil
			}

			spec, err = conf.NewResetSpec(config.Config)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(spec.Active.Source.IsEmpty()).To(BeFalse())

			spec.Active.Size = 16

			grubCfg := filepath.Join(spec.Active.MountPoint, spec.GrubConf)
			err = utils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
			Expect(err).To(BeNil())
			_, err = fs.Create(grubCfg)
			Expect(err).To(BeNil())

			reset = action.NewResetAction(config, spec)
		})

		It("Successfully resets on non-squashfs recovery", func() {
			config.Reboot = true
			Expect(reset.Run()).To(BeNil())
			Expect(runner.IncludesCmds([][]string{{"reboot", "-f"}}))
		})
		It("Successfully resets on non-squashfs recovery including persistent data", func() {
			config.PowerOff = true
			spec.FormatPersistent = true
			spec.FormatOEM = true
			Expect(reset.Run()).To(BeNil())
			Expect(runner.IncludesCmds([][]string{{"poweroff", "-f"}}))
		})
		It("Successfully resets from a squashfs recovery image", Label("channel"), func() {
			err := utils.MkdirAll(config.Fs, constants.IsoBaseTree, constants.DirPerm)
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
