/*
   Copyright © 2021 SUSE LLC

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

package elemental_test

import (
	"errors"
	"fmt"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"os"
	"path/filepath"
	"testing"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"

	"github.com/jaypipes/ghw/pkg/block"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/vfst"
	"k8s.io/mount-utils"
)

const printOutput = `BYT;
/dev/loop0:50593792s:loopback:512:512:gpt:Loopback device:;`
const partTmpl = `
%d:%ss:%ss:2048s:ext4::type=83;`

func TestElementalSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Elemental test suite")
}

var _ = Describe("Elemental", Label("elemental"), func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var logger sdkTypes.KairosLogger
	var syscall v1.SyscallInterface
	var client *v1mock.FakeHTTPClient
	var mounter *v1mock.ErrorMounter
	var fs *vfst.TestFS
	var cleanup func()
	var extractor *v1mock.FakeImageExtractor

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		logger = sdkTypes.NewNullLogger()
		fs, cleanup, _ = vfst.NewTestFS(nil)
		extractor = v1mock.NewFakeImageExtractor(logger)
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(client),
			agentConfig.WithImageExtractor(extractor),
		)
	})
	AfterEach(func() { cleanup() })
	Describe("MountRWPartition", Label("mount"), func() {
		var el *elemental.Elemental
		var parts v1.ElementalPartitions
		BeforeEach(func() {
			spec := &v1.InstallSpec{}
			parts = agentConfig.NewInstallElementalPartitions(logger, spec)

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())

			parts.OEM.Path = "/dev/device1"

			el = elemental.NewElemental(config)
		})

		It("Mounts and umounts a partition with RW", func() {
			umount, err := el.MountRWPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(1))
			Expect(lst[0].Opts).To(Equal([]string{"rw"}))

			Expect(umount()).ShouldNot(HaveOccurred())
			lst, _ = mounter.List()
			Expect(len(lst)).To(Equal(0))
		})
		It("Remounts a partition with RW", func() {
			err := el.MountPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(1))

			umount, err := el.MountRWPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ = mounter.List()
			// fake mounter is not merging remounts it just appends
			Expect(len(lst)).To(Equal(2))
			Expect(lst[1].Opts).To(Equal([]string{"remount", "rw"}))

			Expect(umount()).ShouldNot(HaveOccurred())
			lst, _ = mounter.List()
			// Increased once more to remount read-onply
			Expect(len(lst)).To(Equal(3))
			Expect(lst[2].Opts).To(Equal([]string{"remount", "ro"}))
		})
		It("Fails to mount a partition", func() {
			mounter.ErrorOnMount = true
			_, err := el.MountRWPartition(parts.OEM)
			Expect(err).Should(HaveOccurred())
		})
		It("Fails to remount a partition", func() {
			err := el.MountPartition(parts.OEM)
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(1))

			mounter.ErrorOnMount = true
			_, err = el.MountRWPartition(parts.OEM)
			Expect(err).Should(HaveOccurred())
			lst, _ = mounter.List()
			Expect(len(lst)).To(Equal(1))
		})
	})
	Describe("MountPartitions", Label("MountPartitions", "disk", "partition", "mount"), func() {
		var el *elemental.Elemental
		var parts v1.ElementalPartitions
		BeforeEach(func() {
			spec := &v1.InstallSpec{}
			parts = agentConfig.NewInstallElementalPartitions(logger, spec)

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())

			parts.OEM.Path = "/dev/device2"
			parts.Recovery.Path = "/dev/device3"
			parts.State.Path = "/dev/device4"
			parts.Persistent.Path = "/dev/device5"

			el = elemental.NewElemental(config)
		})

		It("Mounts disk partitions", func() {
			err := el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(4))
		})

		It("Mounts disk partitions excluding recovery", func() {
			err := el.MountPartitions(parts.PartitionsByMountPoint(false, parts.Recovery))
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			for _, i := range lst {
				Expect(i.Path).NotTo(Equal("/dev/device3"))
			}
		})

		It("Fails if some partition resists to mount ", func() {
			mounter.ErrorOnMount = true
			err := el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).NotTo(BeNil())
		})

		It("Fails if oem partition is not found ", func() {
			parts.OEM.Path = ""
			err := el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("UnmountPartitions", Label("UnmountPartitions", "disk", "partition", "unmount"), func() {
		var el *elemental.Elemental
		var parts v1.ElementalPartitions
		BeforeEach(func() {
			spec := &v1.InstallSpec{}
			parts = agentConfig.NewInstallElementalPartitions(logger, spec)

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())

			parts.OEM.Path = "/dev/device2"
			parts.Recovery.Path = "/dev/device3"
			parts.State.Path = "/dev/device4"
			parts.Persistent.Path = "/dev/device5"

			el = elemental.NewElemental(config)
			err = el.MountPartitions(parts.PartitionsByMountPoint(false))
			Expect(err).ToNot(HaveOccurred())
		})

		It("Unmounts disk partitions", func() {
			err := el.UnmountPartitions(parts.PartitionsByMountPoint(true))
			Expect(err).To(BeNil())
			lst, _ := mounter.List()
			Expect(len(lst)).To(Equal(0))
		})

		It("Fails to unmount disk partitions", func() {
			mounter.ErrorOnUnmount = true
			err := el.UnmountPartitions(parts.PartitionsByMountPoint(true))
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("MountImage", Label("MountImage", "mount", "image"), func() {
		var el *elemental.Elemental
		var img *v1.Image
		BeforeEach(func() {
			el = elemental.NewElemental(config)
			img = &v1.Image{MountPoint: "/some/mountpoint"}
		})

		It("Mounts file system image", func() {
			runner.ReturnValue = []byte("/dev/loop")
			Expect(el.MountImage(img)).To(BeNil())
			Expect(img.LoopDevice).To(Equal("/dev/loop"))
		})

		It("Fails to set a loop device", Label("loop"), func() {
			runner.ReturnError = errors.New("failed to set a loop device")
			Expect(el.MountImage(img)).NotTo(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})

		It("Fails to mount a loop device", Label("loop"), func() {
			runner.ReturnValue = []byte("/dev/loop")
			mounter.ErrorOnMount = true
			Expect(el.MountImage(img)).NotTo(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})
	})

	Describe("UnmountImage", Label("UnmountImage", "mount", "image"), func() {
		var el *elemental.Elemental
		var img *v1.Image
		BeforeEach(func() {
			runner.ReturnValue = []byte("/dev/loop")
			el = elemental.NewElemental(config)
			img = &v1.Image{MountPoint: "/some/mountpoint"}
			Expect(el.MountImage(img)).To(BeNil())
			Expect(img.LoopDevice).To(Equal("/dev/loop"))
		})

		It("Unmounts file system image", func() {
			Expect(el.UnmountImage(img)).To(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})

		It("Fails to unmount a mountpoint", func() {
			mounter.ErrorOnUnmount = true
			Expect(el.UnmountImage(img)).NotTo(BeNil())
		})

		It("Fails to unset a loop device", Label("loop"), func() {
			runner.ReturnError = errors.New("failed to unset a loop device")
			Expect(el.UnmountImage(img)).NotTo(BeNil())
		})
	})

	Describe("CreateFileSystemImage", Label("CreateFileSystemImage", "image"), func() {
		var el *elemental.Elemental
		var img *v1.Image
		BeforeEach(func() {
			img = &v1.Image{
				Label:      cnst.ActiveLabel,
				Size:       32,
				File:       filepath.Join(cnst.StateDir, "cOS", cnst.ActiveImgFile),
				FS:         cnst.LinuxImgFs,
				MountPoint: cnst.ActiveDir,
				Source:     v1.NewDirSrc(cnst.IsoBaseTree),
			}
			_ = fsutils.MkdirAll(fs, cnst.IsoBaseTree, cnst.DirPerm)
			el = elemental.NewElemental(config)
		})

		It("Creates a new file system image", func() {
			_, err := fs.Stat(img.File)
			Expect(err).NotTo(BeNil())
			err = el.CreateFileSystemImage(img)
			Expect(err).To(BeNil())
			stat, err := fs.Stat(img.File)
			Expect(err).To(BeNil())
			Expect(stat.Size()).To(Equal(int64(32 * 1024 * 1024)))
		})

		It("Fails formatting a file system image", Label("format"), func() {
			runner.ReturnError = errors.New("run error")
			_, err := fs.Stat(img.File)
			Expect(err).NotTo(BeNil())
			err = el.CreateFileSystemImage(img)
			Expect(err).NotTo(BeNil())
			_, err = fs.Stat(img.File)
			Expect(err).NotTo(BeNil())
		})
	})

	Describe("FormatPartition", Label("FormatPartition", "partition", "format"), func() {
		It("Reformats an already existing partition", func() {
			el := elemental.NewElemental(config)
			part := &v1.Partition{
				Path:            "/dev/device1",
				FS:              "ext4",
				FilesystemLabel: "MY_LABEL",
			}
			Expect(el.FormatPartition(part)).To(BeNil())
		})

	})
	Describe("PartitionAndFormatDevice", Label("PartitionAndFormatDevice", "partition", "format"), func() {
		var el *elemental.Elemental
		var cInit *v1mock.FakeCloudInitRunner
		var partNum int
		var printOut string
		var failPart bool
		var install *v1.InstallSpec
		var err error

		BeforeEach(func() {
			cInit = &v1mock.FakeCloudInitRunner{ExecStages: []string{}, Error: false}
			config.CloudInitRunner = cInit
			config.Install.Device = "/some/device"
			el = elemental.NewElemental(config)
			install, err = agentConfig.NewInstallSpec(config)
			Expect(err).ToNot(HaveOccurred())
			install.Target = "/some/device"

			err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			_, err = fs.Create("/some/device")
			Expect(err).ToNot(HaveOccurred())
		})

		Describe("Successful run", func() {
			var runFunc func(cmd string, args ...string) ([]byte, error)
			var efiPartCmds, partCmds, biosPartCmds [][]string
			BeforeEach(func() {
				partNum, printOut = 0, printOutput
				err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
				Expect(err).To(BeNil())
				efiPartCmds = [][]string{
					{
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mklabel", "gpt",
					}, {
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mkpart", "efi", "fat32", "2048", "133119", "set", "1", "esp", "on",
					}, {"mkfs.vfat", "-n", "COS_GRUB", "/some/device1"},
				}
				biosPartCmds = [][]string{
					{
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mklabel", "gpt",
					}, {
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mkpart", "bios", "", "2048", "4095", "set", "1", "bios_grub", "on",
					}, {"wipefs", "--all", "/some/device1"},
				}
				// These commands are only valid for EFI case
				partCmds = [][]string{
					{
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mkpart", "oem", "ext4", "133120", "264191",
					}, {"mkfs.ext4", "-L", "COS_OEM", "/some/device2"}, {
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mkpart", "recovery", "ext4", "264192", "673791",
					}, {"mkfs.ext4", "-L", "COS_RECOVERY", "/some/device3"}, {
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mkpart", "state", "ext4", "673792", "2721791",
					}, {"mkfs.ext4", "-L", "COS_STATE", "/some/device4"}, {
						"parted", "--script", "--machine", "--", "/some/device", "unit", "s",
						"mkpart", "persistent", "ext4", "2721792", "100%",
					}, {"mkfs.ext4", "-L", "COS_PERSISTENT", "/some/device5"},
				}

				runFunc = func(cmd string, args ...string) ([]byte, error) {
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
							printOut += fmt.Sprintf(partTmpl, partNum, args[idx+3], args[idx+4])
							_, _ = fs.Create(fmt.Sprintf("/some/device%d", partNum))
						}
						return []byte(printOut), nil
					default:
						return []byte{}, nil
					}
				}
				runner.SideEffect = runFunc
			})

			It("Successfully creates partitions and formats them, EFI boot", func() {
				install.PartTable = v1.GPT
				install.Firmware = v1.EFI
				install.Partitions.SetFirmwarePartitions(v1.EFI, v1.GPT)
				Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
				Expect(runner.MatchMilestones(append(efiPartCmds, partCmds...))).To(BeNil())
			})

			It("Successfully creates partitions and formats them, BIOS boot", func() {
				install.PartTable = v1.GPT
				install.Firmware = v1.BIOS
				install.Partitions.SetFirmwarePartitions(v1.BIOS, v1.GPT)
				Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
				Expect(runner.MatchMilestones(biosPartCmds)).To(BeNil())
			})
		})

		Describe("Run with failures", func() {
			var runFunc func(cmd string, args ...string) ([]byte, error)
			BeforeEach(func() {
				err := fsutils.MkdirAll(fs, "/some", cnst.DirPerm)
				Expect(err).To(BeNil())
				partNum, printOut = 0, printOutput
				runFunc = func(cmd string, args ...string) ([]byte, error) {
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
							printOut += fmt.Sprintf(partTmpl, partNum, args[idx+3], args[idx+4])
							if failPart {
								return []byte{}, errors.New("Failure")
							}
							_, _ = fs.Create(fmt.Sprintf("/some/device%d", partNum))
						}
						return []byte(printOut), nil
					case "mkfs.ext4", "wipefs", "mkfs.vfat":
						return []byte{}, errors.New("Failure")
					default:
						return []byte{}, nil
					}
				}
				runner.SideEffect = runFunc
			})

			It("Fails creating efi partition", func() {
				failPart = true
				Expect(el.PartitionAndFormatDevice(install)).NotTo(BeNil())
				// Failed to create first partition
				Expect(partNum).To(Equal(1))
			})

			It("Fails formatting efi partition", func() {
				failPart = false
				Expect(el.PartitionAndFormatDevice(install)).NotTo(BeNil())
				// Failed to format first partition
				Expect(partNum).To(Equal(1))
			})
		})
	})
	Describe("DeployImage", Label("DeployImage"), func() {
		var el *elemental.Elemental
		var img *v1.Image
		var cmdFail string
		BeforeEach(func() {
			sourceDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).ShouldNot(HaveOccurred())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).ShouldNot(HaveOccurred())
			cmdFail = ""
			el = elemental.NewElemental(config)
			img = &v1.Image{
				FS:         cnst.LinuxImgFs,
				Size:       16,
				Source:     v1.NewDirSrc(sourceDir),
				MountPoint: destDir,
				File:       filepath.Join(destDir, "image.img"),
				Label:      "some_label",
			}
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmdFail == cmd {
					return []byte{}, errors.New("Command failed")
				}
				switch cmd {
				case "losetup":
					return []byte("/dev/loop"), nil
				default:
					return []byte{}, nil
				}
			}
		})
		It("Deploys an image from a directory and leaves it mounted", func() {
			Expect(el.DeployImage(img, true)).To(BeNil())
		})
		It("Deploys an image from a directory and leaves it unmounted", func() {
			Expect(el.DeployImage(img, false)).To(BeNil())
		})
		It("Deploys an squashfs image from a directory", func() {
			img.FS = cnst.SquashFs
			Expect(el.DeployImage(img, true)).To(BeNil())
			Expect(runner.MatchMilestones([][]string{
				{
					"mksquashfs", "/tmp/elemental-tmp", "/tmp/elemental/image.img",
					"-b", "1024k", "-comp", "gzip",
				},
			}))
		})
		It("Deploys a file image and mounts it", func() {
			sourceImg := "/source.img"
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).To(BeNil())
			img.Source = v1.NewFileSrc(sourceImg)
			img.MountPoint = destDir
			Expect(el.DeployImage(img, true)).To(BeNil())
		})
		It("Deploys a file image and fails to mount it", func() {
			sourceImg := "/source.img"
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).To(BeNil())
			img.Source = v1.NewFileSrc(sourceImg)
			img.MountPoint = destDir
			mounter.ErrorOnMount = true
			_, err = el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Deploys a file image and fails to label it", func() {
			sourceImg := "/source.img"
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			destDir, err := fsutils.TempDir(fs, "", "elemental")
			Expect(err).To(BeNil())
			img.Source = v1.NewFileSrc(sourceImg)
			img.MountPoint = destDir
			cmdFail = "tune2fs"
			_, err = el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Fails creating the squashfs filesystem", func() {
			cmdFail = "mksquashfs"
			img.FS = cnst.SquashFs
			_, err := el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
			Expect(runner.MatchMilestones([][]string{
				{
					"mksquashfs", "/tmp/elemental-tmp", "/tmp/elemental/image.img",
					"-b", "1024k", "-comp", "gzip",
				},
			}))
		})
		It("Fails formatting the image", func() {
			cmdFail = "mkfs.ext2"
			_, err := el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Fails mounting the image", func() {
			mounter.ErrorOnMount = true
			_, err := el.DeployImage(img, true)
			Expect(err).NotTo(BeNil())
		})
		It("Fails unmounting the image after copying", func() {
			mounter.ErrorOnUnmount = true
			_, err := el.DeployImage(img, false)
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("DumpSource", Label("dump"), func() {
		var e *elemental.Elemental
		var destDir string
		BeforeEach(func() {
			var err error
			e = elemental.NewElemental(config)
			destDir, err = fsutils.TempDir(fs, "", "elemental")
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("Copies files from a directory source", func() {
			rsyncCount := 0
			src := ""
			dest := ""
			runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
				if cmd == cnst.Rsync {
					rsyncCount += 1
					src = args[len(args)-2]
					dest = args[len(args)-1]

				}
				return []byte{}, nil
			}
			_, err := e.DumpSource("/dest", v1.NewDirSrc("/source"))
			Expect(err).ShouldNot(HaveOccurred())
			Expect(rsyncCount).To(Equal(1))
			Expect(src).To(HaveSuffix("/source/"))
			Expect(dest).To(HaveSuffix("/dest/"))
		})
		It("Unpacks a docker image to target", Label("docker"), func() {
			_, err := e.DumpSource(destDir, v1.NewDockerSrc("docker/image:latest"))
			Expect(err).To(BeNil())
		})
		It("Unpacks a docker image to target with cosign validation", Label("docker", "cosign"), func() {
			config.Cosign = true
			_, err := e.DumpSource(destDir, v1.NewDockerSrc("docker/image:latest"))
			Expect(err).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{"cosign", "verify", "docker/image:latest"}}))
		})
		It("Fails cosign validation", Label("cosign"), func() {
			runner.ReturnError = errors.New("cosign error")
			config.Cosign = true
			_, err := e.DumpSource(destDir, v1.NewDockerSrc("docker/image:latest"))
			Expect(err).NotTo(BeNil())
			Expect(runner.CmdsMatch([][]string{{"cosign", "verify", "docker/image:latest"}}))
		})
		It("Copies image file to target", func() {
			sourceImg := "/source.img"
			destFile := filepath.Join(destDir, "active.img")
			_, err := fs.Create(sourceImg)
			Expect(err).To(BeNil())
			_, err = e.DumpSource(destFile, v1.NewFileSrc(sourceImg))
			Expect(err).To(BeNil())
			Expect(runner.IncludesCmds([][]string{{cnst.Rsync}}))
		})
		It("Fails to copy, source file is not present", func() {
			_, err := e.DumpSource("whatever", v1.NewFileSrc("/source.img"))
			Expect(err).NotTo(BeNil())
		})
	})
	Describe("CheckActiveDeployment", Label("check"), func() {
		It("deployment found", func() {
			ghwTest := v1mock.GhwMock{}
			disk := block.Disk{Name: "device", Partitions: []*block.Partition{
				{
					Name:            "device1",
					FilesystemLabel: cnst.ActiveLabel,
				},
			}}
			ghwTest.AddDisk(disk)
			ghwTest.CreateDevices()
			defer ghwTest.Clean()
			runner.ReturnValue = []byte(
				fmt.Sprintf(
					`{"blockdevices": [{"label": "%s", "type": "loop", "path": "/some/device"}]}`,
					cnst.ActiveLabel,
				),
			)
			e := elemental.NewElemental(config)
			Expect(e.CheckActiveDeployment([]string{cnst.ActiveLabel, cnst.PassiveLabel})).To(BeTrue())
		})

		It("Should not error out", func() {
			runner.ReturnValue = []byte("")
			e := elemental.NewElemental(config)
			Expect(e.CheckActiveDeployment([]string{cnst.ActiveLabel, cnst.PassiveLabel})).To(BeFalse())
		})
	})
	Describe("SelinuxRelabel", Label("SelinuxRelabel", "selinux"), func() {
		var policyFile string
		var relabelCmd []string
		BeforeEach(func() {
			// to mock the existance of setfiles command on non selinux hosts
			err := fsutils.MkdirAll(fs, "/usr/sbin", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			sbin, err := fs.RawPath("/usr/sbin")
			Expect(err).ShouldNot(HaveOccurred())

			path := os.Getenv("PATH")
			os.Setenv("PATH", fmt.Sprintf("%s:%s", sbin, path))
			_, err = fs.Create("/usr/sbin/setfiles")
			Expect(err).ShouldNot(HaveOccurred())
			err = fs.Chmod("/usr/sbin/setfiles", 0777)
			Expect(err).ShouldNot(HaveOccurred())

			// to mock SELinux policy files
			policyFile = filepath.Join(cnst.SELinuxTargetedPolicyPath, "policy.31")
			err = fsutils.MkdirAll(fs, filepath.Dir(cnst.SELinuxTargetedContextFile), cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(cnst.SELinuxTargetedContextFile)
			Expect(err).ShouldNot(HaveOccurred())
			err = fsutils.MkdirAll(fs, cnst.SELinuxTargetedPolicyPath, cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(policyFile)
			Expect(err).ShouldNot(HaveOccurred())

			relabelCmd = []string{
				"setfiles", "-c", policyFile, "-e", "/dev", "-e", "/proc", "-e", "/sys",
				"-F", cnst.SELinuxTargetedContextFile, "/",
			}
		})
		It("does nothing if the context file is not found", func() {
			err := fs.Remove(cnst.SELinuxTargetedContextFile)
			Expect(err).ShouldNot(HaveOccurred())

			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("/", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{}}))
		})
		It("does nothing if the policy file is not found", func() {
			err := fs.Remove(policyFile)
			Expect(err).ShouldNot(HaveOccurred())

			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("/", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{{}}))
		})
		It("relabels the current root", func() {
			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())

			runner.ClearCmds()
			Expect(c.SelinuxRelabel("/", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
		It("fails to relabel the current root", func() {
			runner.ReturnError = errors.New("setfiles failure")
			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("", true)).NotTo(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
		It("ignores relabel failures", func() {
			runner.ReturnError = errors.New("setfiles failure")
			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("", false)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
		It("relabels the given root-tree path", func() {
			contextFile := filepath.Join("/root", cnst.SELinuxTargetedContextFile)
			err := fsutils.MkdirAll(fs, filepath.Dir(contextFile), cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(contextFile)
			Expect(err).ShouldNot(HaveOccurred())
			policyFile = filepath.Join("/root", policyFile)
			err = fsutils.MkdirAll(fs, filepath.Join("/root", cnst.SELinuxTargetedPolicyPath), cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			_, err = fs.Create(policyFile)
			Expect(err).ShouldNot(HaveOccurred())

			relabelCmd = []string{
				"setfiles", "-c", policyFile, "-F", "-r", "/root", contextFile, "/root",
			}

			c := elemental.NewElemental(config)
			Expect(c.SelinuxRelabel("/root", true)).To(BeNil())
			Expect(runner.CmdsMatch([][]string{relabelCmd})).To(BeNil())
		})
	})
	Describe("GetIso", Label("GetIso", "iso"), func() {
		var e *elemental.Elemental
		BeforeEach(func() {
			e = elemental.NewElemental(config)
		})
		It("Gets the iso and returns the temporary where it is stored", func() {
			tmpDir, err := fsutils.TempDir(fs, "", "elemental-test")
			Expect(err).To(BeNil())
			err = fs.WriteFile(fmt.Sprintf("%s/fake.iso", tmpDir), []byte("Hi"), cnst.FilePerm)
			Expect(err).To(BeNil())
			iso := fmt.Sprintf("%s/fake.iso", tmpDir)
			isoDir, err := e.GetIso(iso)
			Expect(err).To(BeNil())
			// Confirm that the iso is stored in isoDir
			fsutils.Exists(fs, filepath.Join(isoDir, "cOs.iso"))
		})
		It("Fails if it cant find the iso", func() {
			iso := "whatever"
			e := elemental.NewElemental(config)
			_, err := e.GetIso(iso)
			Expect(err).ToNot(BeNil())
		})
		It("Fails if it cannot mount the iso", func() {
			mounter.ErrorOnMount = true
			tmpDir, err := fsutils.TempDir(fs, "", "elemental-test")
			Expect(err).To(BeNil())
			err = fs.WriteFile(fmt.Sprintf("%s/fake.iso", tmpDir), []byte("Hi"), cnst.FilePerm)
			Expect(err).To(BeNil())
			iso := fmt.Sprintf("%s/fake.iso", tmpDir)
			_, err = e.GetIso(iso)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("mount error"))
		})
	})
	Describe("UpdateSourcesFormDownloadedISO", Label("iso"), func() {
		var e *elemental.Elemental
		var activeImg, recoveryImg *v1.Image
		BeforeEach(func() {
			activeImg, recoveryImg = nil, nil
			e = elemental.NewElemental(config)
		})
		It("updates active image", func() {
			activeImg = &v1.Image{}
			err := e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(activeImg.Source.IsDir()).To(BeTrue())
			Expect(activeImg.Source.Value()).To(Equal("/some/dir/rootfs"))
			Expect(recoveryImg).To(BeNil())
		})
		It("updates active and recovery image", func() {
			activeImg = &v1.Image{File: "activeFile"}
			recoveryImg = &v1.Image{}
			err := e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(recoveryImg.Source.IsFile()).To(BeTrue())
			Expect(recoveryImg.Source.Value()).To(Equal("activeFile"))
			Expect(recoveryImg.Label).To(Equal(cnst.SystemLabel))
			Expect(activeImg.Source.IsDir()).To(BeTrue())
			Expect(activeImg.Source.Value()).To(Equal("/some/dir/rootfs"))
		})
		It("updates recovery only image", func() {
			recoveryImg = &v1.Image{}
			isoMnt := "/some/dir/iso"
			err := fsutils.MkdirAll(fs, isoMnt, cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			recoverySquash := filepath.Join(isoMnt, cnst.RecoverySquashFile)
			_, err = fs.Create(recoverySquash)
			Expect(err).ShouldNot(HaveOccurred())
			err = e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(recoveryImg.Source.IsFile()).To(BeTrue())
			Expect(recoveryImg.Source.Value()).To(Equal(recoverySquash))
			Expect(activeImg).To(BeNil())
		})
		It("fails to update recovery from active file", func() {
			recoveryImg = &v1.Image{}
			err := e.UpdateSourcesFormDownloadedISO("/some/dir", activeImg, recoveryImg)
			Expect(err).Should(HaveOccurred())
		})
	})
	Describe("CloudConfig", Label("CloudConfig", "cloud-config"), func() {
		var e *elemental.Elemental
		BeforeEach(func() {
			e = elemental.NewElemental(config)
		})
		It("Copies the cloud config file", func() {
			testString := "In a galaxy far far away..."
			cloudInit := []string{"/config.yaml"}
			err := fs.WriteFile(cloudInit[0], []byte(testString), cnst.FilePerm)
			Expect(err).To(BeNil())
			Expect(err).To(BeNil())

			err = e.CopyCloudConfig(cloudInit)
			Expect(err).To(BeNil())
			configFilePath := fmt.Sprintf("%s/90_custom.yaml", cnst.OEMDir)
			copiedFile, err := fs.ReadFile(configFilePath)
			Expect(err).To(BeNil())
			Expect(copiedFile).To(ContainSubstring(testString))
			stat, err := fs.Stat(configFilePath)
			Expect(err).To(BeNil())
			Expect(int(stat.Mode().Perm())).To(Equal(cnst.ConfigPerm))

		})
		It("Doesnt do anything if the config file is not set", func() {
			err := e.CopyCloudConfig([]string{})
			Expect(err).To(BeNil())
		})
	})
	Describe("SetDefaultGrubEntry", Label("SetDefaultGrubEntry", "grub"), func() {
		It("Sets the default grub entry without issues", func() {
			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountpoint", "default_entry")).To(BeNil())
		})
		It("does nothing on empty default entry and no /etc/os-release", func() {
			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "")).To(BeNil())
			// No grub2-editenv command called
			Expect(runner.CmdsMatch([][]string{{"grub2-editenv"}})).NotTo(BeNil())
		})
		It("loads /etc/os-release on empty default entry", func() {
			err := fsutils.MkdirAll(config.Fs, "/imgMountPoint/etc", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = config.Fs.WriteFile("/imgMountPoint/etc/os-release", []byte("GRUB_ENTRY_NAME=test"), cnst.FilePerm)
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "")).To(BeNil())
			// Calls grub2-editenv with the loaded content from /etc/os-release
			editEnv := utils.FindCommand("grub2-editenv", []string{"grub2-editenv", "grub-editenv"})
			Expect(runner.CmdsMatch([][]string{
				{editEnv, "/mountpoint/grub_oem_env", "set", "default_menu_entry=test"},
			})).To(BeNil())
		})
		It("Fails setting grubenv", func() {
			runner.ReturnError = errors.New("failure")
			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "default_entry")).NotTo(BeNil())
		})
	})
	Describe("FindKernelInitrd", Label("find"), func() {
		BeforeEach(func() {
			err := fsutils.MkdirAll(fs, "/path/boot", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
		})
		It("finds kernel and initrd files", func() {
			_, err := fs.Create("/path/boot/initrd")
			Expect(err).ShouldNot(HaveOccurred())

			_, err = fs.Create("/path/boot/vmlinuz")
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			k, i, err := el.FindKernelInitrd("/path")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(k).To(Equal("/path/boot/vmlinuz"))
			Expect(i).To(Equal("/path/boot/initrd"))
		})
		It("fails if no initrd is found", func() {
			_, err := fs.Create("/path/boot/vmlinuz")
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			_, _, err = el.FindKernelInitrd("/path")
			Expect(err).Should(HaveOccurred())
		})
		It("fails if no kernel is found", func() {
			_, err := fs.Create("/path/boot/initrd")
			Expect(err).ShouldNot(HaveOccurred())

			el := elemental.NewElemental(config)
			_, _, err = el.FindKernelInitrd("/path")
			Expect(err).Should(HaveOccurred())
		})
	})
	Describe("DeactivateDevices", Label("blkdeactivate"), func() {
		It("calls blkdeactivat", func() {
			el := elemental.NewElemental(config)
			err := el.DeactivateDevices()
			Expect(err).ShouldNot(HaveOccurred())
			Expect(runner.CmdsMatch([][]string{{
				"blkdeactivate", "--lvmoptions", "retry,wholevg",
				"--dmoptions", "force,retry", "--errors",
			}})).To(BeNil())
		})
	})
})

// PathInMountPoints will check if the given path is in the mountPoints list
func pathInMountPoints(mounter mount.Interface, path string) bool {
	mountPoints, _ := mounter.List()
	for _, m := range mountPoints {
		if path == m.Path {
			return true
		}
	}
	return false
}
