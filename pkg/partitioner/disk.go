package partitioner

import (
	"fmt"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/sanity-io/litter"
)

type Disk struct {
	*disk.Disk
	logger logger.KairosLogger
}

func (d *Disk) NewPartitionTable(partType string, parts partitions.PartitionList) error {
	d.logger.Infof("Creating partition table for partition type %s", partType)
	var table partition.Table
	switch partType {
	case sdkConstants.GPT:
		table = &gpt.Table{
			ProtectiveMBR:      true,
			GUID:               cnst.DiskUUID, // Set know predictable UUID
			Partitions:         kairosPartsToDiskfsGPTParts(parts, d.Size, d.LogicalBlocksize),
			LogicalSectorSize:  int(d.LogicalBlocksize),
			PhysicalSectorSize: int(d.PhysicalBlocksize),
		}
	default:
		return fmt.Errorf("invalid partition type: %s", partType)
	}
	err := d.Partition(table)
	if err != nil {
		return err
	}
	d.logger.Infof("Created partition table for partition type %s", partType)
	return nil
}

func getSectorEndFromSize(start, size uint64, sectorSize int64) uint64 {
	return (size / uint64(sectorSize)) + start - 1
}

func kairosPartsToDiskfsGPTParts(parts partitions.PartitionList, diskSize int64, sectorSize int64) []*gpt.Partition {
	var partitions []*gpt.Partition
	for index, part := range parts {
		var start uint64
		var end uint64
		var size uint64
		if len(partitions) == 0 {
			// first partition, align to 1Mb
			start = 1024 * 1024 / uint64(sectorSize)
		} else {
			// get latest partition end, sum 1
			start = partitions[len(partitions)-1].End + 1
		}

		// part.Size 0 means take over whats left on the disk
		if part.Size == 0 {
			// Remember to add the 1Mb alignment to total size
			// This will be on bytes already no need to transform it
			var sizeUsed = uint64(1024 * 1024)
			for _, p := range partitions {
				sizeUsed = sizeUsed + p.Size
			}
			// leave 1Mb at the end for backup GPT header
			size = uint64(diskSize) - sizeUsed - uint64(1024*1024)
		} else {
			// Change it to bytes
			// If its the last partition to do, leave 1 Mb at the end for backup GPT header
			if index == len(parts)-1 {
				size = uint64(part.Size*1024*1024) - uint64(1024*1024)
			} else {
				size = uint64(part.Size * 1024 * 1024)
			}

		}

		end = getSectorEndFromSize(start, size, sectorSize)

		if part.Name == sdkConstants.EfiPartName && part.FS == sdkConstants.EfiFs {
			// EFI boot partition
			partitions = append(partitions, &gpt.Partition{
				Start:      start,
				End:        end,
				Type:       gpt.EFISystemPartition,
				Size:       size,                                                         // partition size in bytes
				GUID:       uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(), // set know predictable UUID
				Name:       part.Name,
				Attributes: 0x1, // system partition flag
			})
		} else if part.Name == sdkConstants.BiosPartName {
			// Non-EFI boot partition
			partitions = append(partitions, &gpt.Partition{
				Start:      start,
				End:        end,
				Type:       gpt.BIOSBoot,
				Size:       size,                                                         // partition size in bytes
				GUID:       uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(), // set know predictable UUID
				Name:       part.Name,
				Attributes: 0x4, // legacy bios bootable flag
			})
		} else {
			// Other partitions
			partitions = append(partitions, &gpt.Partition{
				Start: start,
				End:   end,
				Type:  gpt.LinuxFilesystem,
				Size:  size,
				GUID:  uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(),
				Name:  part.Name,
			})
		}
	}
	return partitions
}

type DiskOptions func(d *Disk) error

func WithLogger(logger logger.KairosLogger) func(d *Disk) error {
	return func(d *Disk) error {
		d.logger = logger
		return nil
	}
}

func NewDisk(device string, opts ...DiskOptions) (*Disk, error) {
	d, err := diskfs.Open(device)
	if err != nil {
		return nil, err
	}
	dev := &Disk{d, logger.NewKairosLogger("partitioner", "info", false)}

	for _, opt := range opts {
		if err := opt(dev); err != nil {
			return nil, err
		}
	}

	dev.logger.Debugf("Initialized new disk from device %s", litter.Sdump(dev))
	return dev, nil
}
