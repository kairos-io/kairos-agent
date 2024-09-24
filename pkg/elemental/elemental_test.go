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
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	sc "syscall"
	"testing"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	ghwMock "github.com/kairos-io/kairos-sdk/ghw/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sanity-io/litter"
	"github.com/twpayne/go-vfs/v4/vfst"
)

func TestElementalSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Elemental test suite")
}

var _ = Describe("Elemental", Label("elemental"), func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var logger sdkTypes.KairosLogger
	var syscall *v1mock.FakeSyscall
	var cl *v1mock.FakeHTTPClient
	var mounter *v1mock.ErrorMounter
	var fs *vfst.TestFS
	var cleanup func()
	var extractor *v1mock.FakeImageExtractor
	var memLog *bytes.Buffer
	var devLoopInt int

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		devLoopInt = 44
		syscall.SideEffectSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err sc.Errno) {
			// Trap the call for getting a free loop device number
			if trap == sc.SYS_IOCTL && a2 == unix.LOOP_CTL_GET_FREE {
				// This is a "get free loop device" syscall
				// We return 44 so it gets the /dev/loop44 because we are cool like that
				// Also we can check below that indeed it set that device as expected
				return uintptr(devLoopInt), 0, sc.Errno(syscall.ReturnValue)
			}
			return 0, 0, sc.Errno(syscall.ReturnValue)
		}
		mounter = v1mock.NewErrorMounter()
		cl = &v1mock.FakeHTTPClient{}
		fs, cleanup, _ = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control":                    "",
			fmt.Sprintf("/dev/loop%d", devLoopInt): "",
		})
		extractor = v1mock.NewFakeImageExtractor(logger)
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(cl),
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
			img = &v1.Image{MountPoint: "/some/mountpoint", File: "/image.file"}
			Expect(fs.WriteFile("/image.file", []byte{}, cnst.FilePerm)).To(Succeed())
		})

		It("Mounts file system image", func() {
			err := el.MountImage(img)
			Expect(err).To(BeNil())
			Expect(img.LoopDevice).To(Equal(fmt.Sprintf("/dev/loop%d", devLoopInt)), litter.Sdump(img))
		})

		It("Fails to set a loop device", Label("loop"), func() {
			// Return error on syscall call
			syscall.ReturnValue = 10
			Expect(el.MountImage(img)).NotTo(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})

		It("Fails to mount a loop device", Label("loop"), func() {
			mounter.ErrorOnMount = true
			Expect(el.MountImage(img)).NotTo(BeNil())
			Expect(img.LoopDevice).To(Equal(""))
		})
	})
	Describe("UnmountImage", Label("UnmountImage", "mount", "image"), func() {
		var el *elemental.Elemental
		var img *v1.Image
		BeforeEach(func() {
			el = elemental.NewElemental(config)
			img = &v1.Image{MountPoint: "/some/mountpoint", File: "/image.file"}
			Expect(fs.WriteFile("/image.file", []byte{}, cnst.FilePerm)).To(Succeed())
			Expect(el.MountImage(img)).To(BeNil())
			Expect(img.LoopDevice).To(Equal(fmt.Sprintf("/dev/loop%d", devLoopInt)))
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
			syscall.ReturnValue = 10
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
			part := &sdkTypes.Partition{
				Path:            "/dev/device1",
				FS:              "ext4",
				FilesystemLabel: "MY_LABEL",
			}
			Expect(el.FormatPartition(part)).To(BeNil())
		})

	})
	Describe("PartitionAndFormatDevice", Label("PartitionAndFormatDevice", "partition", "format"), func() {
		var cInit *v1mock.FakeCloudInitRunner
		var install *v1.InstallSpec
		var err error
		var el *elemental.Elemental
		var tmpDir string

		BeforeEach(func() {
			cInit = &v1mock.FakeCloudInitRunner{ExecStages: []string{}, Error: false}
			config.CloudInitRunner = cInit
			tmpDir, err = os.MkdirTemp("", "elements-*")
			Expect(err).To(BeNil())
			Expect(os.RemoveAll(filepath.Join(tmpDir, "/test.img"))).ToNot(HaveOccurred())
			// at least 2Gb in size as state is set to 1G
			_, err = diskfs.Create(filepath.Join(tmpDir, "/test.img"), 2*1024*1024*1024, diskfs.Raw, 512)
			Expect(err).ToNot(HaveOccurred())
			config.Install.Device = filepath.Join(tmpDir, "/test.img")
			install, err = agentConfig.NewInstallSpec(config)
			Expect(err).ToNot(HaveOccurred())
			install.Target = filepath.Join(tmpDir, "/test.img")
			el = elemental.NewElemental(config)
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tmpDir)).ToNot(HaveOccurred())
		})

		It("Successfully creates partitions and formats them, EFI boot", func() {
			install.PartTable = v1.GPT
			install.Firmware = v1.EFI
			Expect(install.Partitions.SetFirmwarePartitions(v1.EFI, v1.GPT)).To(BeNil())
			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
			disk, err := diskfs.Open(filepath.Join(tmpDir, "/test.img"), diskfs.WithOpenMode(diskfs.ReadOnly))
			defer disk.Close()
			Expect(err).ToNot(HaveOccurred())
			// check that its type GPT
			Expect(reflect.TypeOf(disk.Table)).To(Equal(reflect.TypeOf(&gpt.Table{})))
			// Expect the disk UUID to be constant
			Expect(strings.ToLower(disk.Table.UUID())).To(Equal(strings.ToLower(cnst.DiskUUID)))
			// 5 partitions (boot, oem, recovery, state and persistent)
			Expect(len(disk.Table.GetPartitions())).To(Equal(5))
			// Cast the boot partition into specific type to check the type and such
			part := disk.Table.GetPartitions()[0]
			partition, ok := part.(*gpt.Partition)
			Expect(ok).To(BeTrue())
			// Should be efi type
			Expect(partition.Type).To(Equal(gpt.EFISystemPartition))
			// should have boot label
			Expect(partition.Name).To(Equal(cnst.EfiPartName))
			// Should have predictable UUID
			Expect(strings.ToLower(partition.UUID())).To(Equal(strings.ToLower(uuid.NewV5(uuid.NamespaceURL, cnst.EfiLabel).String())))
			// Check the rest have the proper types
			for i := 1; i < len(disk.Table.GetPartitions()); i++ {
				part := disk.Table.GetPartitions()[i]
				partition, ok := part.(*gpt.Partition)
				Expect(ok).To(BeTrue())
				// all of them should have the Linux fs type
				Expect(partition.Type).To(Equal(gpt.LinuxFilesystem))
			}
		})
		It("Successfully creates partitions and formats them, BIOS boot", func() {
			install.PartTable = v1.GPT
			install.Firmware = v1.BIOS
			Expect(install.Partitions.SetFirmwarePartitions(v1.BIOS, v1.GPT)).To(BeNil())
			Expect(el.PartitionAndFormatDevice(install)).To(BeNil())
			disk, err := diskfs.Open(filepath.Join(tmpDir, "/test.img"), diskfs.WithOpenMode(diskfs.ReadOnly))
			defer disk.Close()
			Expect(err).ToNot(HaveOccurred())
			// check that its type GPT
			Expect(reflect.TypeOf(disk.Table)).To(Equal(reflect.TypeOf(&gpt.Table{})))
			// Expect the disk UUID to be constant
			Expect(strings.ToLower(disk.Table.UUID())).To(Equal(strings.ToLower(cnst.DiskUUID)))
			// 5 partitions (boot, oem, recovery, state and persistent)
			Expect(len(disk.Table.GetPartitions())).To(Equal(5))
			// Cast the boot partition into specific type to check the type and such
			part := disk.Table.GetPartitions()[0]
			partition, ok := part.(*gpt.Partition)
			Expect(ok).To(BeTrue())
			// Should be BIOS boot type
			Expect(partition.Type).To(Equal(gpt.BIOSBoot))
			// should have boot label
			Expect(partition.Name).To(Equal(cnst.BiosPartName))
			// Should have predictable UUID
			Expect(strings.ToLower(partition.UUID())).To(Equal(strings.ToLower(uuid.NewV5(uuid.NamespaceURL, cnst.EfiLabel).String())))
			for i := 1; i < len(disk.Table.GetPartitions()); i++ {
				part := disk.Table.GetPartitions()[i]
				partition, ok := part.(*gpt.Partition)
				Expect(ok).To(BeTrue())
				// all of them should have the Linux fs type
				Expect(partition.Type).To(Equal(gpt.LinuxFilesystem))
			}
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
				default:
					GinkgoWriter.Println(fmt.Sprintf("Command %s called but we dont catch it", cmd))
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
			ghwTest := ghwMock.GhwMock{}
			disk := sdkTypes.Disk{Name: "device", Partitions: []*sdkTypes.Partition{
				{
					Name:            "device1",
					FilesystemLabel: cnst.ActiveLabel,
				},
			}}
			ghwTest.AddDisk(disk)
			ghwTest.CreateDevices()

			runner.ReturnValue = []byte(
				fmt.Sprintf(
					`{"blockdevices": [{"label": "%s", "type": "loop", "path": "/some/device"}]}`,
					cnst.ActiveLabel,
				),
			)
			e := elemental.NewElemental(config)
			Expect(e.CheckActiveDeployment([]string{cnst.ActiveLabel, cnst.PassiveLabel})).To(BeTrue())

			ghwTest.Clean()
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
			iso := "http://whatever"
			cl.Error = true
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
			Expect(config.Fs.Mkdir("/tmp", cnst.DirPerm)).To(BeNil())
			Expect(el.SetDefaultGrubEntry("/tmp", "/imgMountpoint", "dio")).To(BeNil())
			varsParsed, err := utils.ReadPersistentVariables(filepath.Join("/tmp", cnst.GrubOEMEnv), config.Fs)
			Expect(err).To(BeNil())
			Expect(varsParsed["default_menu_entry"]).To(Equal("dio"))
		})
		It("does nothing on empty default entry and no /etc/os-release", func() {
			el := elemental.NewElemental(config)
			Expect(config.Fs.Mkdir("/mountpoint", cnst.DirPerm)).To(BeNil())
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "")).To(BeNil())
			_, err := utils.ReadPersistentVariables(filepath.Join("/tmp", cnst.GrubOEMEnv), config.Fs)
			// Because it didnt do anything due to the entry being empty, the file should not be there
			Expect(err).ToNot(BeNil())
			_, err = config.Fs.Stat(filepath.Join("/tmp", cnst.GrubOEMEnv))
			Expect(err).ToNot(BeNil())
		})
		It("loads /etc/os-release on empty default entry", func() {
			err := fsutils.MkdirAll(config.Fs, "/imgMountPoint/etc", cnst.DirPerm)
			Expect(err).ShouldNot(HaveOccurred())
			err = config.Fs.WriteFile("/imgMountPoint/etc/os-release", []byte("GRUB_ENTRY_NAME=test"), cnst.FilePerm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/mountpoint", cnst.DirPerm)).To(BeNil())

			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("/mountpoint", "/imgMountPoint", "")).To(BeNil())
			varsParsed, err := utils.ReadPersistentVariables(filepath.Join("/mountpoint", cnst.GrubOEMEnv), config.Fs)
			Expect(err).To(BeNil())
			Expect(varsParsed["default_menu_entry"]).To(Equal("test"))

		})
		It("Fails setting grubenv", func() {
			el := elemental.NewElemental(config)
			Expect(el.SetDefaultGrubEntry("nonexisting", "nonexisting", "default_entry")).NotTo(BeNil())
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
