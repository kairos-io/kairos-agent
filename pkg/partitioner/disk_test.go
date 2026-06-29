/*
Copyright © 2026 Kairos authors

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

package partitioner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
)

const (
	mib        = 1024 * 1024
	sectorSize = 512
)

// createDiskImage creates a sparse file of the given size to act as a fake disk.
func createDiskImage(sizeMiB int64) string {
	dir := ginkgo.GinkgoT().TempDir()
	img := filepath.Join(dir, "disk.img")
	f, err := os.Create(img)
	Expect(err).ToNot(HaveOccurred())
	Expect(f.Truncate(sizeMiB * mib)).To(Succeed())
	Expect(f.Close()).To(Succeed())
	return img
}

var _ = ginkgo.Describe("Disk", ginkgo.Label("disk"), func() {
	var log logger.KairosLogger

	ginkgo.BeforeEach(func() {
		log = logger.NewNullLogger()
	})

	ginkgo.Describe("NewDisk", func() {
		ginkgo.It("initializes a disk from a device image", func() {
			img := createDiskImage(10)
			d, err := NewDisk(img)
			Expect(err).ToNot(HaveOccurred())
			Expect(d).ToNot(BeNil())
			Expect(d.Size).To(Equal(int64(10 * mib)))
			Expect(d.LogicalBlocksize).To(Equal(int64(sectorSize)))
		})

		ginkgo.It("applies the WithLogger option", func() {
			img := createDiskImage(10)
			d, err := NewDisk(img, WithLogger(log))
			Expect(err).ToNot(HaveOccurred())
			Expect(d).ToNot(BeNil())
			Expect(d.logger).To(Equal(log))
		})

		ginkgo.It("fails when the device does not exist", func() {
			d, err := NewDisk("/some/nonexisting/device")
			Expect(err).To(HaveOccurred())
			Expect(d).To(BeNil())
		})

		ginkgo.It("fails when an option returns an error", func() {
			img := createDiskImage(10)
			failingOpt := func(d *Disk) error { return errors.New("option failed") }
			d, err := NewDisk(img, failingOpt)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("option failed"))
			Expect(d).To(BeNil())
		})
	})

	ginkgo.Describe("NewPartitionTable", func() {
		ginkgo.It("fails with an invalid partition table type", func() {
			img := createDiskImage(10)
			d, err := NewDisk(img, WithLogger(log))
			Expect(err).ToNot(HaveOccurred())
			err = d.NewPartitionTable("msdos", partitions.PartitionList{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid partition type: msdos"))
		})

		ginkgo.It("creates a GPT partition table with the expected partitions", func() {
			img := createDiskImage(100)
			d, err := NewDisk(img, WithLogger(log))
			Expect(err).ToNot(HaveOccurred())

			parts := partitions.PartitionList{
				{Name: sdkConstants.EfiPartName, FilesystemLabel: "COS_GRUB", Size: 20, FS: sdkConstants.EfiFs},
				{Name: sdkConstants.BiosPartName, FilesystemLabel: "bios", Size: 2, FS: ""},
				{Name: "state", FilesystemLabel: "COS_STATE", Size: 0, FS: "ext4"},
			}
			Expect(d.NewPartitionTable(sdkConstants.GPT, parts)).To(Succeed())

			table, err := d.GetPartitionTable()
			Expect(err).ToNot(HaveOccurred())
			gptTable, ok := table.(*gpt.Table)
			Expect(ok).To(BeTrue())
			// GUIDs are read back from disk in uppercase
			Expect(strings.ToLower(gptTable.GUID)).To(Equal(cnst.DiskUUID))

			tableParts := gptTable.Partitions
			Expect(len(tableParts)).To(BeNumerically(">=", 3))

			// EFI partition, 1MiB aligned start, system partition attribute
			Expect(tableParts[0].Name).To(Equal(sdkConstants.EfiPartName))
			Expect(tableParts[0].Type).To(Equal(gpt.EFISystemPartition))
			Expect(tableParts[0].Attributes).To(Equal(uint64(0x1)))
			Expect(tableParts[0].Start).To(Equal(uint64(mib / sectorSize)))
			Expect(tableParts[0].End).To(Equal(uint64(20*mib/sectorSize + mib/sectorSize - 1)))
			Expect(strings.ToLower(tableParts[0].GUID)).To(Equal(uuid.NewV5(uuid.NamespaceURL, "COS_GRUB").String()))

			// BIOS partition, starts right after EFI, legacy bios bootable attribute
			Expect(tableParts[1].Name).To(Equal(sdkConstants.BiosPartName))
			Expect(tableParts[1].Type).To(Equal(gpt.BIOSBoot))
			Expect(tableParts[1].Attributes).To(Equal(uint64(0x4)))
			Expect(tableParts[1].Start).To(Equal(tableParts[0].End + 1))
			Expect(tableParts[1].End).To(Equal(tableParts[1].Start + 2*mib/sectorSize - 1))

			// State partition fills the rest of the disk minus 1MiB for the backup GPT header
			Expect(tableParts[2].Name).To(Equal("state"))
			Expect(tableParts[2].Type).To(Equal(gpt.LinuxFilesystem))
			Expect(tableParts[2].Start).To(Equal(tableParts[1].End + 1))
			expectedStateSize := uint64(100*mib) - uint64(23*mib) - uint64(mib)
			Expect(tableParts[2].End).To(Equal(tableParts[2].Start + expectedStateSize/sectorSize - 1))
			Expect(strings.ToLower(tableParts[2].GUID)).To(Equal(uuid.NewV5(uuid.NamespaceURL, "COS_STATE").String()))
		})

		ginkgo.It("fails to write the partition table on a read-only device", func() {
			img := createDiskImage(100)
			roDisk, err := diskfs.Open(img, diskfs.WithOpenMode(diskfs.ReadOnly))
			Expect(err).ToNot(HaveOccurred())
			d := &Disk{roDisk, log}

			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			err = d.NewPartitionTable(sdkConstants.GPT, parts)
			Expect(err).To(HaveOccurred())
		})
	})

	ginkgo.Describe("getSectorEndFromSize", func() {
		ginkgo.It("computes the end sector from start and size", func() {
			// 1MiB at sector 2048 with 512 sector size occupies 2048 sectors
			Expect(getSectorEndFromSize(2048, mib, sectorSize)).To(Equal(uint64(4095)))
			Expect(getSectorEndFromSize(0, mib, sectorSize)).To(Equal(uint64(2047)))
			Expect(getSectorEndFromSize(2048, 20*mib, sectorSize)).To(Equal(uint64(43007)))
		})
	})

	ginkgo.Describe("kairosPartsToDiskfsGPTParts", func() {
		ginkgo.It("returns an empty list for no partitions", func() {
			Expect(kairosPartsToDiskfsGPTParts(partitions.PartitionList{}, 100*mib, sectorSize)).To(BeEmpty())
		})

		ginkgo.It("reserves 1MiB at the end when the last partition has an explicit size", func() {
			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(1))
			Expect(gptParts[0].Start).To(Equal(uint64(2048)))
			// 10MiB requested but last partition gives up 1MiB for the backup GPT header
			Expect(gptParts[0].Size).To(Equal(uint64(9 * mib)))
			Expect(gptParts[0].End).To(Equal(uint64(2048 + 9*mib/sectorSize - 1)))
			Expect(gptParts[0].Type).To(Equal(gpt.LinuxFilesystem))
			Expect(gptParts[0].Index).To(Equal(1))
			Expect(gptParts[0].Attributes).To(BeZero())
		})

		ginkgo.It("expands a zero-sized partition to fill the remaining disk", func() {
			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 30, FS: "ext4"},
				{Name: "persistent", FilesystemLabel: "COS_PERSISTENT", Size: 0, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(2))
			Expect(gptParts[0].Size).To(Equal(uint64(30 * mib)))
			// 100MiB disk - 1MiB alignment - 30MiB used - 1MiB backup header = 68MiB
			Expect(gptParts[1].Size).To(Equal(uint64(68 * mib)))
			Expect(gptParts[1].Start).To(Equal(gptParts[0].End + 1))
			Expect(gptParts[1].Index).To(Equal(2))
		})

		ginkgo.It("sets EFI specific type and attributes only for vfat efi partitions", func() {
			parts := partitions.PartitionList{
				{Name: sdkConstants.EfiPartName, FilesystemLabel: "COS_GRUB", Size: 20, FS: sdkConstants.EfiFs},
				{Name: sdkConstants.EfiPartName, FilesystemLabel: "COS_GRUB2", Size: 20, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(2))
			Expect(gptParts[0].Type).To(Equal(gpt.EFISystemPartition))
			Expect(gptParts[0].Attributes).To(Equal(uint64(0x1)))
			// efi name without vfat filesystem is treated as a regular partition
			Expect(gptParts[1].Type).To(Equal(gpt.LinuxFilesystem))
			Expect(gptParts[1].Attributes).To(BeZero())
		})

		ginkgo.It("sets BIOS specific type and attributes for the bios partition", func() {
			parts := partitions.PartitionList{
				{Name: sdkConstants.BiosPartName, FilesystemLabel: "bios", Size: 1},
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts).To(HaveLen(2))
			Expect(gptParts[0].Type).To(Equal(gpt.BIOSBoot))
			Expect(gptParts[0].Attributes).To(Equal(uint64(0x4)))
			Expect(gptParts[0].Name).To(Equal(sdkConstants.BiosPartName))
		})

		ginkgo.It("assigns predictable UUIDs based on the filesystem label", func() {
			parts := partitions.PartitionList{
				{Name: "oem", FilesystemLabel: "COS_OEM", Size: 10, FS: "ext4"},
			}
			gptParts := kairosPartsToDiskfsGPTParts(parts, 100*mib, sectorSize)
			Expect(gptParts[0].GUID).To(Equal(uuid.NewV5(uuid.NamespaceURL, "COS_OEM").String()))
		})
	})
})
