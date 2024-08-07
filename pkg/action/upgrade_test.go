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
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"path/filepath"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4"
	"github.com/twpayne/go-vfs/v4/vfst"
)

var _ = Describe("Runtime Actions", func() {
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
	var ghwTest v1mock.GhwMock
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		logger.SetLevel("debug")
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
			agentConfig.WithPlatform("linux/amd64"),
		)
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Upgrade Action", Label("upgrade"), func() {
		var spec *v1.UpgradeSpec
		var upgrade *action.UpgradeAction
		var memLog *bytes.Buffer
		activeImg := fmt.Sprintf("%s/cOS/%s", constants.RunningStateDir, constants.ActiveImgFile)
		passiveImg := fmt.Sprintf("%s/cOS/%s", constants.RunningStateDir, constants.PassiveImgFile)
		recoveryImgSquash := fmt.Sprintf("%s/cOS/%s", constants.LiveDir, constants.RecoverySquashFile)
		recoveryImg := fmt.Sprintf("%s/cOS/%s", constants.LiveDir, constants.RecoveryImgFile)

		BeforeEach(func() {
			memLog = &bytes.Buffer{}
			logger = sdkTypes.NewBufferLogger(memLog)
			extractor = v1mock.NewFakeImageExtractor(logger)
			config.Logger = logger
			config.ImageExtractor = extractor
			logger.SetLevel("debug")

			// Create paths used by tests
			fsutils.MkdirAll(fs, fmt.Sprintf("%s/cOS", constants.RunningStateDir), constants.DirPerm)
			fsutils.MkdirAll(fs, fmt.Sprintf("%s/cOS", constants.LiveDir), constants.DirPerm)

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
						MountPoint:      constants.RunningStateDir,
					},
					{
						Name:            "loop0",
						FilesystemLabel: "COS_ACTIVE",
						Type:            "ext4",
					},
					{
						Name:            "device5",
						FilesystemLabel: "COS_RECOVERY",
						Type:            "ext4",
						MountPoint:      constants.LiveDir,
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
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		Describe(fmt.Sprintf("Booting from %s", constants.ActiveLabel), Label("active_label"), func() {
			var err error
			BeforeEach(func() {
				spec, err = agentConfig.NewUpgradeSpec(config)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, filepath.Join(spec.Active.MountPoint, "etc"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				// Write proc/cmdline so we can detect what we booted from
				err = fs.WriteFile("/proc/cmdline", []byte(constants.ActiveLabel), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fs.WriteFile(
					filepath.Join(spec.Active.MountPoint, "etc", "os-release"),
					[]byte("GRUB_ENTRY_NAME=TESTOS"),
					constants.FilePerm,
				)
				Expect(err).ShouldNot(HaveOccurred())

				spec.Active.Size = 10
				spec.Passive.Size = 10
				spec.Recovery.Size = 10

				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					if command == "mv" && args[0] == "-f" && args[1] == activeImg && args[2] == passiveImg {
						// we doing backup, do the "move"
						source, _ := fs.ReadFile(activeImg)
						_ = fs.WriteFile(passiveImg, source, constants.FilePerm)
						_ = fs.RemoveAll(activeImg)
					}
					if command == "mv" && args[0] == "-f" && args[1] == spec.Active.File && args[2] == activeImg {
						// we doing the image substitution, do the "move"
						source, _ := fs.ReadFile(spec.Active.File)
						_ = fs.WriteFile(activeImg, source, constants.FilePerm)
						_ = fs.RemoveAll(spec.Active.File)
					}
					return []byte{}, nil
				}
				config.Runner = runner
				// Create fake active/passive files
				_ = fs.WriteFile(activeImg, []byte("active"), constants.FilePerm)
				_ = fs.WriteFile(passiveImg, []byte("passive"), constants.FilePerm)
				// Mount state partition as it is expected to be mounted when booting from active
				mounter.Mount("device2", constants.RunningStateDir, "auto", []string{"ro"})
			})
			AfterEach(func() {
				_ = fs.RemoveAll(activeImg)
				_ = fs.RemoveAll(passiveImg)
				mounter.Unmount("device2")
			})
			It("Fails if some hook fails and strict is set", func() {
				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					if command == "cat" && args[0] == "/proc/cmdline" {
						return []byte(constants.ActiveLabel), nil
					}
					return []byte{}, nil
				}
				config.Strict = true
				cloudInit.Error = true
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).To(HaveOccurred())
				// Make sure is a cloud init error!
				Expect(err.Error()).To(ContainSubstring("cloud init"))
			})
			It("Successfully upgrades from docker image", Label("docker"), func() {
				spec.Active.Source = v1.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our os-release value
				Expect(memLog).To(ContainSubstring("default_menu_entry=TESTOS"))

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should be the config.ImgSize as its truncated from the upgrade
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())
			})
			It("Successfully reboots after upgrade from docker image", Label("docker"), func() {
				spec.Active.Source = v1.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				By("Upgrading")
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())
				By("Checking the log")
				// Check that the rebrand worked with our os-release value
				Expect(memLog).To(ContainSubstring("default_menu_entry=TESTOS"))

				By("checking active image")
				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should be the config.ImgSize as its truncated from the upgrade
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				By("Checking passive image")
				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))
				By("checking transition image")
				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())
				By("checking it called reboot")
			})
			It("Successfully powers off after upgrade from docker image", Label("docker"), func() {
				spec.Active.Source = v1.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our os-release value
				Expect(memLog).To(ContainSubstring("default_menu_entry=TESTOS"))

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should be the config.ImgSize as its truncated from the upgrade
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())
			})
			It("Successfully upgrades from directory", Label("directory"), func() {
				dirSrc, _ := fsutils.TempDir(fs, "", "elementalupgrade")
				// Create the dir on real os as rsync works on the real os
				defer fs.RemoveAll(dirSrc)
				spec.Active.Source = v1.NewDirSrc(dirSrc)
				// create a random file on it
				err := fs.WriteFile(fmt.Sprintf("%s/file.file", dirSrc), []byte("something"), constants.FilePerm)
				Expect(err).ToNot(HaveOccurred())

				upgrade = action.NewUpgradeAction(config, spec)
				err = upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our os-release value
				Expect(memLog).To(ContainSubstring("default_menu_entry=TESTOS"))

				// Not much that we can create here as the dir copy was done on the real os, but we do the rest of the ops on a mem one
				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should not be empty
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Should have backed up active to passive
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.img
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				// This should be a backup so it should read active
				Expect(f).To(ContainSubstring("active"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())

			})
		})
		Describe(fmt.Sprintf("Booting from %s", constants.PassiveLabel), Label("passive_label"), func() {
			var err error
			BeforeEach(func() {
				spec, err = agentConfig.NewUpgradeSpec(config)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, filepath.Join(spec.Active.MountPoint, "etc"), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				err = fs.WriteFile(
					filepath.Join(spec.Active.MountPoint, "etc", "os-release"),
					[]byte("GRUB_ENTRY_NAME=TESTOS"),
					constants.FilePerm,
				)
				Expect(err).ShouldNot(HaveOccurred())

				err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())

				// Write proc/cmdline so we can detect what we booted from
				err = fs.WriteFile("/proc/cmdline", []byte(constants.PassiveLabel), constants.FilePerm)
				Expect(err).ShouldNot(HaveOccurred())

				spec.Active.Size = 10
				spec.Passive.Size = 10
				spec.Recovery.Size = 10

				runner.SideEffect = func(command string, args ...string) ([]byte, error) {
					if command == "cat" && args[0] == "/proc/cmdline" {
						return []byte(constants.PassiveLabel), nil
					}
					if command == "mv" && args[0] == "-f" && args[1] == spec.Active.File && args[2] == activeImg {
						// we doing the image substitution, do the "move"
						source, _ := fs.ReadFile(spec.Active.File)
						_ = fs.WriteFile(activeImg, source, constants.FilePerm)
						_ = fs.RemoveAll(spec.Active.File)
					}
					if command == "mv" && args[0] == "-f" && args[1] == activeImg && args[2] == passiveImg {
						// If this command was called then its a complete failure as it tried to copy active into passive
						StopTrying("Passive was overwritten").Now()
					}
					return []byte{}, nil
				}
				config.Runner = runner
				// Create fake active/passive files
				_ = fs.WriteFile(activeImg, []byte("active"), constants.FilePerm)
				_ = fs.WriteFile(passiveImg, []byte("passive"), constants.FilePerm)
				// Mount state partition as it is expected to be mounted when booting from active
				mounter.Mount("device2", constants.RunningStateDir, "auto", []string{"ro"})
			})
			AfterEach(func() {
				_ = fs.RemoveAll(activeImg)
				_ = fs.RemoveAll(passiveImg)
			})
			It("does not backup active img to passive", Label("docker"), func() {
				spec.Active.Source = v1.NewDockerSrc("alpine")
				upgrade = action.NewUpgradeAction(config, spec)
				err := upgrade.Run()
				Expect(err).ToNot(HaveOccurred())

				// Check that the rebrand worked with our os-release value
				Expect(memLog).To(ContainSubstring("default_menu_entry=TESTOS"))

				// This should be the new image
				info, err := fs.Stat(activeImg)
				Expect(err).ToNot(HaveOccurred())
				// Image size should not be empty
				Expect(info.Size()).To(BeNumerically("==", int64(spec.Active.Size*1024*1024)))
				Expect(info.IsDir()).To(BeFalse())

				// Passive should have not been touched
				info, err = fs.Stat(passiveImg)
				Expect(err).ToNot(HaveOccurred())
				// Should be a tiny image as it should only contain our text
				// As this was generated by us at the start test and moved by the upgrade from active.iomg
				Expect(info.Size()).To(BeNumerically(">", 0))
				Expect(info.Size()).To(BeNumerically("<", int64(spec.Active.Size*1024*1024)))
				f, _ := fs.ReadFile(passiveImg)
				Expect(f).To(ContainSubstring("passive"))

				// Expect transition image to be gone
				_, err = fs.Stat(spec.Active.File)
				Expect(err).To(HaveOccurred())

			})
		})
		Describe(fmt.Sprintf("Booting from %s", constants.RecoveryLabel), Label("recovery_label"), func() {
			Describe("Using squashfs", Label("squashfs"), func() {
				var err error
				BeforeEach(func() {
					// Mount recovery partition as it is expected to be mounted when booting from recovery
					mounter.Mount("device5", constants.LiveDir, "auto", []string{"ro"})
					// Create recoveryImgSquash so ti identifies that we are using squash recovery
					err = fs.WriteFile(recoveryImgSquash, []byte("recovery"), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err = agentConfig.NewUpgradeSpec(config)
					Expect(err).ShouldNot(HaveOccurred())
					spec.Active.Size = 10
					spec.Passive.Size = 10
					spec.Recovery.Size = 10

					spec.RecoveryUpgrade = true

					err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())

					// Write proc/cmdline so we can detect what we booted from
					err = fs.WriteFile("/proc/cmdline", []byte(constants.RecoveryLabel), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cat" && args[0] == "/proc/cmdline" {
							return []byte(constants.RecoveryLabel), nil
						}
						if command == "mksquashfs" && args[1] == spec.Recovery.File {
							// create the transition img for squash to fake it
							_, _ = fs.Create(spec.Recovery.File)
						}
						if command == "mv" && args[0] == "-f" && args[1] == spec.Recovery.File && args[2] == recoveryImgSquash {
							// fake "move"
							f, _ := fs.ReadFile(spec.Recovery.File)
							_ = fs.WriteFile(recoveryImgSquash, f, constants.FilePerm)
							_ = fs.RemoveAll(spec.Recovery.File)
						}
						return []byte{}, nil
					}
					config.Runner = runner
				})
				It("Successfully upgrades recovery from docker image", Label("docker"), func() {
					// This should be the old image
					info, err := fs.Stat(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be empty
					Expect(info.Size()).To(BeNumerically(">", 0))
					Expect(info.IsDir()).To(BeFalse())
					f, _ := fs.ReadFile(recoveryImgSquash)
					Expect(f).To(ContainSubstring("recovery"))

					spec.Recovery.Source = v1.NewDockerSrc("alpine")
					upgrade = action.NewUpgradeAction(config, spec)
					err = upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// This should be the new image
					info, err = fs.Stat(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be empty
					Expect(info.Size()).To(BeNumerically("==", 0))
					Expect(info.IsDir()).To(BeFalse())
					f, _ = fs.ReadFile(recoveryImgSquash)
					Expect(f).ToNot(ContainSubstring("recovery"))

					// Transition squash should not exist
					info, err = fs.Stat(spec.Recovery.File)
					Expect(err).To(HaveOccurred())

				})
				It("Successfully upgrades recovery from directory", Label("directory"), func() {
					srcDir, _ := fsutils.TempDir(fs, "", "elemental")
					// create a random file on it
					_ = fs.WriteFile(fmt.Sprintf("%s/file.file", srcDir), []byte("something"), constants.FilePerm)

					spec.Recovery.Source = v1.NewDirSrc(srcDir)
					upgrade = action.NewUpgradeAction(config, spec)
					err := upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// This should be the new image
					info, err := fs.Stat(recoveryImgSquash)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be empty
					Expect(info.Size()).To(BeNumerically("==", 0))
					Expect(info.IsDir()).To(BeFalse())

					// Transition squash should not exist
					info, err = fs.Stat(spec.Recovery.File)
					Expect(err).To(HaveOccurred())

				})
			})
			Describe("Not using squashfs", Label("non-squashfs"), func() {
				var err error
				BeforeEach(func() {
					// Create recoveryImg so it identifies that we are using nonsquash recovery
					err = fs.WriteFile(recoveryImg, []byte("recovery"), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err = agentConfig.NewUpgradeSpec(config)
					Expect(err).ShouldNot(HaveOccurred())

					spec.Active.Size = 10
					spec.Passive.Size = 10
					spec.Recovery.Size = 10

					spec.RecoveryUpgrade = true

					err = fsutils.MkdirAll(config.Fs, "/proc", constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())

					// Write proc/cmdline so we can detect what we booted from
					err = fs.WriteFile("/proc/cmdline", []byte(constants.RecoveryLabel), constants.FilePerm)
					Expect(err).ShouldNot(HaveOccurred())

					runner.SideEffect = func(command string, args ...string) ([]byte, error) {
						if command == "cat" && args[0] == "/proc/cmdline" {
							return []byte(constants.RecoveryLabel), nil
						}
						if command == "mv" && args[0] == "-f" && args[1] == spec.Recovery.File && args[2] == recoveryImg {
							// fake "move"
							f, _ := fs.ReadFile(spec.Recovery.File)
							_ = fs.WriteFile(recoveryImg, f, constants.FilePerm)
							_ = fs.RemoveAll(spec.Recovery.File)
						}
						return []byte{}, nil
					}
					config.Runner = runner
					_ = fs.WriteFile(recoveryImg, []byte("recovery"), constants.FilePerm)
					// Mount recovery partition as it is expected to be mounted when booting from recovery
					mounter.Mount("device5", constants.LiveDir, "auto", []string{"ro"})
				})
				It("Successfully upgrades recovery from docker image", Label("docker"), func() {
					// This should be the old image
					info, err := fs.Stat(recoveryImg)
					Expect(err).ToNot(HaveOccurred())
					// Image size should not be empty
					Expect(info.Size()).To(BeNumerically(">", 0))
					Expect(info.Size()).To(BeNumerically("<", int64(spec.Recovery.Size*1024*1024)))
					Expect(info.IsDir()).To(BeFalse())
					f, _ := fs.ReadFile(recoveryImg)
					Expect(f).To(ContainSubstring("recovery"))

					spec.Recovery.Source = v1.NewDockerSrc("apline")

					upgrade = action.NewUpgradeAction(config, spec)
					err = upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// Should have created recovery image
					info, err = fs.Stat(recoveryImg)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be default size
					Expect(info.Size()).To(BeNumerically("==", int64(spec.Recovery.Size*1024*1024)))

					// Expect the rest of the images to not be there
					for _, img := range []string{activeImg, passiveImg, recoveryImgSquash} {
						_, err := fs.Stat(img)
						Expect(err).To(HaveOccurred())
					}
				})
				It("Successfully upgrades recovery from directory", Label("directory"), func() {
					srcDir, _ := fsutils.TempDir(fs, "", "elemental")
					// create a random file on it
					_ = fs.WriteFile(fmt.Sprintf("%s/file.file", srcDir), []byte("something"), constants.FilePerm)

					spec.Recovery.Source = v1.NewDirSrc(srcDir)

					upgrade = action.NewUpgradeAction(config, spec)
					err := upgrade.Run()
					Expect(err).ToNot(HaveOccurred())

					// This should be the new image
					info, err := fs.Stat(recoveryImg)
					Expect(err).ToNot(HaveOccurred())
					// Image size should be default size
					Expect(info.Size()).To(BeNumerically("==", int64(spec.Recovery.Size*1024*1024)))
					Expect(info.IsDir()).To(BeFalse())

					// Transition squash should not exist
					info, err = fs.Stat(spec.Recovery.File)
					Expect(err).To(HaveOccurred())
				})
			})
		})
	})
})
