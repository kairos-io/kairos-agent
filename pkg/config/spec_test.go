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

package config_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/sanity-io/litter"
	"github.com/twpayne/go-vfs/v4/vfst"
	"k8s.io/mount-utils"
)

var _ = Describe("Types", Label("types", "config"), func() {
	Describe("Config", func() {
		var err error
		var cleanup func()
		var fs *vfst.TestFS
		var mounter *v1mock.ErrorMounter
		var runner *v1mock.FakeRunner
		var client *v1mock.FakeHTTPClient
		var sysc *v1mock.FakeSyscall
		var logger sdkTypes.KairosLogger
		var ci *v1mock.FakeCloudInitRunner
		var c *config.Config
		BeforeEach(func() {
			fs, cleanup, err = vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			mounter = v1mock.NewErrorMounter()
			runner = v1mock.NewFakeRunner()
			client = &v1mock.FakeHTTPClient{}
			sysc = &v1mock.FakeSyscall{}
			logger = sdkTypes.NewNullLogger()
			ci = &v1mock.FakeCloudInitRunner{}
			c = config.NewConfig(
				config.WithFs(fs),
				config.WithMounter(mounter),
				config.WithRunner(runner),
				config.WithSyscall(sysc),
				config.WithLogger(logger),
				config.WithCloudInitRunner(ci),
				config.WithClient(client),
				config.WithPlatform("linux/arm64"),
			)
			c.Install = &config.Install{}
			c.Bundles = config.Bundles{}
			c.Config = collector.Config{}
			fmt.Println(litter.Sdump(c))
		})
		AfterEach(func() {
			cleanup()
		})
		Describe("ConfigOptions", func() {
			It("Sets the proper interfaces in the config struct", func() {
				Expect(c.Fs).To(Equal(fs))
				Expect(c.Mounter).To(Equal(mounter))
				Expect(c.Runner).To(Equal(runner))
				Expect(c.Syscall).To(Equal(sysc))
				Expect(c.Logger).To(Equal(logger))
				Expect(c.CloudInitRunner).To(Equal(ci))
				Expect(c.Client).To(Equal(client))
				Expect(c.Platform.OS).To(Equal("linux"))
				Expect(c.Platform.Arch).To(Equal("arm64"))
				Expect(c.Platform.GolangArch).To(Equal("arm64"))
			})
			It("Sets the runner if we dont pass one", func() {
				fs, cleanup, err := vfst.NewTestFS(nil)
				defer cleanup()
				Expect(err).ToNot(HaveOccurred())
				c := config.NewConfig(
					config.WithFs(fs),
					config.WithMounter(mounter),
				)
				Expect(c.Fs).To(Equal(fs))
				Expect(c.Mounter).To(Equal(mounter))
				Expect(c.Runner).ToNot(BeNil())
			})
			It("defaults to sane platform if the platform is broken", func() {
				c = config.NewConfig(
					config.WithFs(fs),
					config.WithMounter(mounter),
					config.WithRunner(runner),
					config.WithSyscall(sysc),
					config.WithLogger(logger),
					config.WithCloudInitRunner(ci),
					config.WithClient(client),
					config.WithPlatform("wwwwwww"),
				)
				Expect(c.Platform.OS).To(Equal("linux"))
				Expect(c.Platform.Arch).To(Equal("x86_64"))
				Expect(c.Platform.GolangArch).To(Equal("amd64"))
			})
		})
		Describe("ConfigOptions no mounter specified", Label("mount", "mounter"), func() {
			It("should use the default mounter", Label("systemctl"), func() {
				runner := v1mock.NewFakeRunner()
				sysc := &v1mock.FakeSyscall{}
				logger := sdkTypes.NewNullLogger()
				c := config.NewConfig(
					config.WithRunner(runner),
					config.WithSyscall(sysc),
					config.WithLogger(logger),
				)
				Expect(c.Mounter).To(Equal(mount.New(constants.MountBinary)))
			})
		})
		Describe("Config", func() {
			cfg := config.NewConfig(config.WithMounter(mounter))
			Expect(cfg.Mounter).To(Equal(mounter))
			Expect(cfg.Runner).NotTo(BeNil())
		})
		Describe("InstallSpec", func() {
			It("sets installation defaults from install efi media with recovery", Label("install", "efi"), func() {
				// Set EFI firmware detection
				err = fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDevice), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(constants.EfiDevice)
				Expect(err).ShouldNot(HaveOccurred())

				// Set ISO base tree detection
				err = fsutils.MkdirAll(fs, filepath.Dir(constants.IsoBaseTree), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(constants.IsoBaseTree)
				Expect(err).ShouldNot(HaveOccurred())

				// Set recovery image detection detection
				recoveryImgFile := filepath.Join(constants.LiveDir, constants.RecoverySquashFile)
				err = fsutils.MkdirAll(fs, filepath.Dir(recoveryImgFile), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(recoveryImgFile)
				Expect(err).ShouldNot(HaveOccurred())

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(v1.EFI))
				Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
				Expect(spec.Recovery.Source.Value()).To(Equal(recoveryImgFile))
				Expect(spec.PartTable).To(Equal(v1.GPT))

				// No firmware partitions added yet
				Expect(spec.Partitions.EFI).To(BeNil())

				// Adding firmware partitions
				err = spec.Partitions.SetFirmwarePartitions(spec.Firmware, spec.PartTable)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(spec.Partitions.EFI).NotTo(BeNil())
			})
			It("sets installation defaults from install bios media without recovery", Label("install", "bios"), func() {
				// Set ISO base tree detection
				err = fsutils.MkdirAll(fs, filepath.Dir(constants.IsoBaseTree), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(constants.IsoBaseTree)
				Expect(err).ShouldNot(HaveOccurred())

				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(v1.BIOS))
				Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
				Expect(spec.Recovery.Source.Value()).To(Equal(spec.Active.File))
				Expect(spec.PartTable).To(Equal(v1.GPT))

				// No firmware partitions added yet
				Expect(spec.Partitions.BIOS).To(BeNil())

				// Adding firmware partitions
				err = spec.Partitions.SetFirmwarePartitions(spec.Firmware, spec.PartTable)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(spec.Partitions.BIOS).NotTo(BeNil())
			})
			It("fails if not in installation media or without source", Label("install"), func() {
				// Should fail if not on installation media and no source specified
				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Sanitize()).To(HaveOccurred())

			})
			It("sets installation defaults without being on installation media but with source", Label("install"), func() {
				c.Install.Source = "oci:test:latest"
				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(v1.BIOS))
				fmt.Println(litter.Sdump(spec))
				Expect(spec.Active.Source.IsEmpty()).To(BeFalse())
				Expect(spec.Recovery.Source.Value()).To(Equal(spec.Active.File))
				Expect(spec.PartTable).To(Equal(v1.GPT))
				Expect(spec.Sanitize()).ToNot(HaveOccurred())
			})
			It("sets installation defaults without being on installation media and no source, fails sanitize", Label("install"), func() {
				spec, err := config.NewInstallSpec(c)
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Firmware).To(Equal(v1.BIOS))
				fmt.Println(litter.Sdump(spec))
				Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
				Expect(spec.Recovery.Source.Value()).To(Equal(spec.Active.File))
				Expect(spec.PartTable).To(Equal(v1.GPT))
				Expect(spec.Sanitize()).To(HaveOccurred())
			})
		})
		Describe("ResetSpec", Label("reset"), func() {
			Describe("Successful executions", func() {
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					mainDisk := sdkTypes.Disk{
						Name: "device",
						Partitions: []*sdkTypes.Partition{
							{
								Name:            "device1",
								FilesystemLabel: constants.EfiLabel,
								FS:              "vfat",
							},
							{
								Name:            "device2",
								FilesystemLabel: constants.OEMLabel,
								FS:              "ext4",
							},
							{
								Name:            "device3",
								FilesystemLabel: constants.RecoveryLabel,
								FS:              "ext4",
							},
							{
								Name:            "device4",
								FilesystemLabel: constants.StateLabel,
								FS:              "ext4",
							},
							{
								Name:            "device5",
								FilesystemLabel: constants.PersistentLabel,
								FS:              "ext4",
							},
						},
					}
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(mainDisk)
					ghwTest.CreateDevices()

					runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
						switch cmd {
						case "cat":
							return []byte(constants.SystemLabel), nil
						default:
							return []byte{}, nil
						}
					}
				})
				AfterEach(func() {
					ghwTest.Clean()
				})
				It("sets reset defaults on efi from squashed recovery", func() {
					// Set EFI firmware detection
					err = fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDevice), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(constants.EfiDevice)
					Expect(err).ShouldNot(HaveOccurred())

					// Set squashfs detection
					err = fsutils.MkdirAll(fs, filepath.Dir(constants.IsoBaseTree), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(constants.IsoBaseTree)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err := config.NewResetSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.Value()).To(Equal(constants.IsoBaseTree))
					Expect(spec.Partitions.EFI.MountPoint).To(Equal(constants.EfiDir))
				})
				It("sets reset defaults on bios from non-squashed recovery", func() {
					// Set non-squashfs recovery image detection
					recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
					err = fsutils.MkdirAll(fs, filepath.Dir(recoveryImg), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(recoveryImg)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err := config.NewResetSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.Value()).To(Equal(recoveryImg))
				})
				It("sets reset defaults on bios from unknown recovery", func() {
					spec, err := config.NewResetSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
				})
			})
			Describe("Failures", func() {
				var bootedFrom string
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					bootedFrom = ""
					runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
						switch cmd {
						case "cat":
							return []byte(bootedFrom), nil
						default:
							return []byte{}, nil
						}
					}

					// Set an empty disk for tests, otherwise reads the hosts hardware
					mainDisk := sdkTypes.Disk{
						Name: "device",
						Partitions: []*sdkTypes.Partition{
							{
								Name:            "device4",
								FilesystemLabel: constants.StateLabel,
								FS:              "ext4",
							},
						},
					}
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(mainDisk)
					ghwTest.CreateDevices()
				})
				AfterEach(func() {
					ghwTest.Clean()
				})
				It("fails to set defaults if not booted from recovery", func() {
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("reset can only be called from the recovery system"))
				})
				It("fails to set defaults if no recovery partition detected", func() {
					bootedFrom = constants.SystemLabel
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("recovery partition not found"))
				})
				It("fails to set defaults if no state partition detected", func() {
					mainDisk := sdkTypes.Disk{
						Name:       "device",
						Partitions: []*sdkTypes.Partition{},
					}
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(mainDisk)
					ghwTest.CreateDevices()
					defer ghwTest.Clean()

					bootedFrom = constants.SystemLabel
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("state partition not found"))
				})
				It("fails to set defaults if no efi partition on efi firmware", func() {
					// Set EFI firmware detection
					err = fsutils.MkdirAll(fs, filepath.Dir(constants.EfiDevice), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(constants.EfiDevice)
					Expect(err).ShouldNot(HaveOccurred())

					bootedFrom = constants.SystemLabel
					_, err := config.NewResetSpec(c)
					Expect(err).Should(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("EFI partition not found"))
				})
			})
		})
		Describe("UpgradeSpec", Label("upgrade"), func() {
			Describe("Successful executions", func() {
				var ghwTest ghwMock.GhwMock
				BeforeEach(func() {
					mainDisk := sdkTypes.Disk{
						Name: "device",
						Partitions: []*sdkTypes.Partition{
							{
								Name:            "device1",
								FilesystemLabel: constants.EfiLabel,
								FS:              "vfat",
							},
							{
								Name:            "device2",
								FilesystemLabel: constants.OEMLabel,
								FS:              "ext4",
							},
							{
								Name:            "device3",
								FilesystemLabel: constants.RecoveryLabel,
								FS:              "ext4",
								MountPoint:      constants.LiveDir,
							},
							{
								Name:            "device4",
								FilesystemLabel: constants.StateLabel,
								FS:              "ext4",
							},
							{
								Name:            "device5",
								FilesystemLabel: constants.PersistentLabel,
								FS:              "ext4",
							},
						},
					}
					ghwTest = ghwMock.GhwMock{}
					ghwTest.AddDisk(mainDisk)
					ghwTest.CreateDevices()
				})
				AfterEach(func() {
					ghwTest.Clean()
				})
				It("sets upgrade defaults for active upgrade", func() {
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Active.Source.IsEmpty()).To(BeTrue())
				})
				It("sets upgrade defaults for non-squashed recovery upgrade", func() {
					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Recovery.Source.IsEmpty()).To(BeTrue())
					Expect(spec.Recovery.FS).To(Equal(constants.LinuxImgFs))
				})
				It("sets upgrade defaults for squashed recovery upgrade", func() {
					//Set squashed recovery detection
					mounter.Mount("device3", constants.LiveDir, "auto", []string{})
					img := filepath.Join(constants.LiveDir, "cOS", constants.RecoverySquashFile)
					err = fsutils.MkdirAll(fs, filepath.Dir(img), constants.DirPerm)
					Expect(err).ShouldNot(HaveOccurred())
					_, err = fs.Create(img)
					Expect(err).ShouldNot(HaveOccurred())

					spec, err := config.NewUpgradeSpec(c)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(spec.Recovery.Source.IsEmpty()).To(BeTrue())
					Expect(spec.Recovery.FS).To(Equal(constants.SquashFs))
				})
			})
		})
		Describe("Config from cloudconfig", Label("cloud-config"), func() {
			var bootedFrom string
			var dir string
			var ghwTest ghwMock.GhwMock

			BeforeEach(func() {
				bootedFrom = ""
				runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
					switch cmd {
					case "cat":
						return []byte(bootedFrom), nil
					default:
						return []byte{}, nil
					}
				}

				dir, err = os.MkdirTemp("", "test-config")
				Expect(err).ToNot(HaveOccurred())
				ccdata := []byte(`#cloud-config
strict: true
install:
  device: /some/device
  skip_copy_kcrypt_plugin: true
  grub-entry-name: "MyCustomOS"
  system:
    size: 666
reset:
  reset-persistent: true
  reset-oem: true
  passive:
    label: MY_LABEL
upgrade:
  recovery: true
  system:
    uri: oci:busybox
  recovery-system:
    uri: oci:busybox
cloud-init-paths:
- /what
`)
				err = os.WriteFile(filepath.Join(dir, "cc.yaml"), ccdata, os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				mainDisk := sdkTypes.Disk{
					Name: "device",
					Partitions: []*sdkTypes.Partition{
						{
							Name:            "device1",
							FilesystemLabel: constants.EfiLabel,
							FS:              "vfat",
						},
						{
							Name:            "device2",
							FilesystemLabel: constants.OEMLabel,
							FS:              "ext4",
						},
						{
							Name:            "device3",
							FilesystemLabel: constants.RecoveryLabel,
							FS:              "ext4",
						},
						{
							Name:            "device4",
							FilesystemLabel: constants.StateLabel,
							FS:              "ext4",
						},
						{
							Name:            "device5",
							FilesystemLabel: constants.PersistentLabel,
							FS:              "ext4",
						},
					},
				}
				ghwTest = ghwMock.GhwMock{}
				ghwTest.AddDisk(mainDisk)
				ghwTest.CreateDevices()

				fs, cleanup, err = vfst.NewTestFS(nil)
				err = fsutils.MkdirAll(fs, filepath.Dir(constants.IsoBaseTree), constants.DirPerm)
				Expect(err).ShouldNot(HaveOccurred())
				_, err = fs.Create(constants.IsoBaseTree)
				Expect(err).ShouldNot(HaveOccurred())
			})

			AfterEach(func() {
				os.RemoveAll(dir)
				ghwTest.Clean()
			})
			It("Reads properly the cloud config for install", func() {
				cfg, err := config.Scan(collector.Directories([]string{dir}...),
					collector.NoLogs,
				)
				cfg.Fs = fs

				Expect(err).ToNot(HaveOccurred())
				// Once we got the cfg override the fs to our test fs
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				installSpec, err := config.ReadInstallSpecFromConfig(cfg)
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Strict).To(BeTrue())
				Expect(cfg.Install.SkipEncryptCopyPlugins).To(BeTrue())
				Expect(cfg.Install.Device).To(Equal("/some/device"))
				Expect(installSpec.Target).To(Equal("/some/device"))
				Expect(installSpec.GrubDefEntry).To(Equal("MyCustomOS"))
				Expect(installSpec.Active.Size).To(Equal(uint(666)))
				Expect(cfg.CloudInitPaths).To(ContainElement("/what"))

			})
			It("Reads properly the cloud config for reset", func() {
				bootedFrom = constants.SystemLabel
				cfg, err := config.Scan(collector.Directories([]string{dir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				// Override the config with our test params
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				spec, err := config.ReadSpecFromCloudConfig(cfg, "reset")
				Expect(err).ToNot(HaveOccurred())
				resetSpec := spec.(*v1.ResetSpec)
				Expect(resetSpec.FormatPersistent).To(BeTrue())
				Expect(resetSpec.FormatOEM).To(BeTrue())
				Expect(resetSpec.Passive.Label).To(Equal("MY_LABEL"))
			})
			It("Reads properly the cloud config for upgrade", func() {
				cfg, err := config.Scan(collector.Directories([]string{dir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				// Override the config with our test params
				cfg.Runner = runner
				cfg.Fs = fs
				cfg.Mounter = mounter
				cfg.CloudInitRunner = ci
				spec, err := config.ReadSpecFromCloudConfig(cfg, "upgrade")
				Expect(err).ToNot(HaveOccurred())
				upgradeSpec := spec.(*v1.UpgradeSpec)
				Expect(upgradeSpec.RecoveryUpgrade()).To(BeTrue())
			})
			It("Fails when a wrong action is read", func() {
				cfg, err := config.Scan(collector.Directories([]string{dir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				_, err = config.ReadSpecFromCloudConfig(cfg, "nope")
				Expect(err).To(HaveOccurred())
			})
			It("Sets info level if its not on the cloud-config", func() {
				// Now again but with no config
				cfg, err := config.Scan(collector.Directories([]string{""}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Logger.GetLevel()).To(Equal(zerolog.InfoLevel))
			})
			It("Sets debug level if its on the cloud-config", func() {
				ccdata := []byte(`#cloud-config
debug: true
`)
				err = os.WriteFile(filepath.Join(dir, "cc.yaml"), ccdata, os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				cfg, err := config.Scan(collector.Directories([]string{dir}...), collector.NoLogs)
				Expect(err).ToNot(HaveOccurred())
				Expect(cfg.Logger.GetLevel()).To(Equal(zerolog.DebugLevel))

			})
		})
		Describe("TestBootedFrom", Label("BootedFrom"), func() {
			It("returns true if we are booting from label FAKELABEL", func() {
				runner.ReturnValue = []byte("")
				Expect(config.BootedFrom(runner, "FAKELABEL")).To(BeFalse())
			})
			It("returns false if we are not booting from label FAKELABEL", func() {
				runner.ReturnValue = []byte("FAKELABEL")
				Expect(config.BootedFrom(runner, "FAKELABEL")).To(BeTrue())
			})
		})
	})
})

func createFileOfSizeInMB(filename string, sizeInMB int) error {
	// Calculate the number of bytes needed to reach the desired size in megabytes
	fileSizeInBytes := int64(sizeInMB) * 1024 * 1024

	// Create the file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Seek to the desired file size
	_, err = file.Seek(fileSizeInBytes-1, 0)
	if err != nil {
		return err
	}

	// Write a single byte to "expand" the file to the desired size
	_, err = file.Write([]byte{0})
	if err != nil {
		return err
	}

	return nil
}

var _ = Describe("GetSourceSize", Label("GetSourceSize"), func() {
	var tempDir string
	var tempFilePath string
	var err error
	var logger sdkTypes.KairosLogger
	var conf *config.Config
	var imageSource *v1.ImageSource
	var memLog bytes.Buffer

	BeforeEach(func() {
		tempDir, err = os.MkdirTemp("/tmp", "kairos-test")
		Expect(err).To(BeNil())

		//logger = sdkTypes.NewNullLogger()
		memLog = bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(&memLog)
		logger.SetLevel("debug")
		conf = config.NewConfig(
			config.WithLogger(logger),
		)

		tempFilePath = filepath.Join(tempDir, "200MB.txt")
		err := createFileOfSizeInMB(tempFilePath, 200)
		Expect(err).To(BeNil())

		imageSource = v1.NewDirSrc(tempDir)
	})

	AfterEach(func() {
		fmt.Println(memLog.String())
		defer os.RemoveAll(tempDir)
	})

	It("doesn't count symlinks more than once", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		err = os.Symlink(tempFilePath, filepath.Join(tempDir, "200MB-symlink.txt"))
		Expect(err).To(BeNil())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).To(Equal(sizeBefore))
	})
	It("Skips the kubernetes host dir when calculating the sizes if set", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		Expect(os.Mkdir(filepath.Join(tempDir, "host"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "host", "what.txt"), 200)).ToNot(HaveOccurred())
		// Set env var like the suc upgrade does
		Expect(os.Setenv("HOST_DIR", filepath.Join(tempDir, "host"))).ToNot(HaveOccurred())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).To(Equal(sizeBefore))
	})
	It("Counts the kubernetes host dir when calculating the sizes if not set", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		Expect(os.Mkdir(filepath.Join(tempDir, "host"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "host", "what.txt"), 200)).ToNot(HaveOccurred())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).ToNot(Equal(sizeBefore))
		Expect(sizeAfter).ToNot(BeZero())
		// Size is 2 files of 200 + 100Mb on top, normalized from bytes to MB
		// So take those 200Mb, converts to bytes by multiplying them (400*1024*1024), then back to MB by dividing
		// what we get (/1000/1000) then we finish by adding and extra 100MB on top, like the GetSourceSize does internally
		Expect(sizeAfter).To(Equal(int64((400 * 1024 * 1024 / 1000 / 1000) + 100)))
	})
	It("Does not skip the dirs if outside of kubernetes", func() {
		sizeBefore, err := config.GetSourceSize(conf, imageSource)
		Expect(err).To(BeNil())
		Expect(sizeBefore).ToNot(BeZero())

		// Not inside kubernetes so it should count this dir
		Expect(os.Mkdir(filepath.Join(tempDir, "run"), os.ModePerm)).ToNot(HaveOccurred())
		Expect(createFileOfSizeInMB(filepath.Join(tempDir, "run", "what.txt"), 200)).ToNot(HaveOccurred())

		sizeAfter, err := config.GetSourceSize(conf, imageSource)
		Expect(err).ToNot(HaveOccurred())
		Expect(sizeAfter).ToNot(Equal(sizeBefore))
		Expect(sizeAfter).ToNot(BeZero())
		Expect(sizeAfter).To(Equal(int64((400 * 1024 * 1024 / 1000 / 1000) + 100)))
	})
})
