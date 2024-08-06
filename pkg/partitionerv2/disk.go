package partitionerv2

import (
	"fmt"
	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/gofrs/uuid"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"github.com/sanity-io/litter"
)

type Disk struct {
	*disk.Disk
	logger sdkTypes.KairosLogger
}

func (d *Disk) NewPartitionTable(partType string, parts v1.PartitionList) error {
	d.logger.Infof("Creating partition table for partition type %s", partType)
	var table partition.Table
	switch partType {
	case v1.GPT:
		table = &gpt.Table{
			LogicalSectorSize:  int(diskfs.SectorSize512),
			PhysicalSectorSize: int(diskfs.SectorSize512),
			ProtectiveMBR:      true,
			GUID:               cnst.DiskUUID, // Set know predictable UUID
			Partitions:         kairosPartsToDiskfsGPTParts(parts, d.Size),
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

func getSectorEndFromSize(start, size uint64) uint64 {
	return (size / uint64(diskfs.SectorSize512)) + start - 1
}

func kairosPartsToDiskfsGPTParts(parts v1.PartitionList, diskSize int64) []*gpt.Partition {
	var partitions []*gpt.Partition
	for _, part := range parts {
		var start uint64
		var end uint64
		var size uint64
		if len(partitions) == 0 {
			// first partition, align to 1Mb
			start = 1024 * 1024 / uint64(diskfs.SectorSize512)
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
			size = uint64(diskSize) - sizeUsed
		} else {
			// Change it to bytes
			size = uint64(part.Size * 1024 * 1024)
		}

		end = getSectorEndFromSize(start, size)

		if part.FS == cnst.EfiFs {
			partitions = append(partitions, &gpt.Partition{
				Start: start,
				End:   end,
				Type:  gpt.EFISystemPartition,
				Size:  size,                                                         // partition size in bytes
				GUID:  uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(), // set know predictable UUID
				Name:  part.FilesystemLabel,
			})
		} else {
			partitions = append(partitions, &gpt.Partition{
				Start: start,
				End:   end,
				Type:  gpt.LinuxFilesystem,
				Size:  size,
				GUID:  uuid.NewV5(uuid.NamespaceURL, part.FilesystemLabel).String(),
				Name:  part.FilesystemLabel,
			})
		}
	}
	return partitions
}

type DiskOptions func(d *Disk) error

func WithLogger(logger sdkTypes.KairosLogger) func(d *Disk) error {
	return func(d *Disk) error {
		d.logger = logger
		return nil
	}
}

func NewDisk(device string, opts ...DiskOptions) *Disk {
	d, err := diskfs.Open(device, diskfs.WithSectorSize(512))
	if err != nil {
		return nil
	}
	dev := &Disk{d, sdkTypes.NewKairosLogger("partitioner", "info", false)}

	for _, opt := range opts {
		if err := opt(dev); err != nil {
			return nil
		}
	}

	dev.logger.Debugf("Initialized new disk from device %s", litter.Sdump(dev))
	return dev
}
