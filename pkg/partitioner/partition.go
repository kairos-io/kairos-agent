package partitioner

import (
	"fmt"
	"github.com/diskfs/go-diskfs/partition/gpt"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"os"
	"path/filepath"
	"syscall"
)

// PartitionAndFormatDevice creates a new empty partition table on target disk
// and applies the configured disk layout by creating and formatting all
// required partitions
func PartitionAndFormatDevice(config *agentConfig.Config, i v1.SharedInstallSpec) error {
	if _, err := os.Stat(i.GetTarget()); os.IsNotExist(err) {
		config.Logger.Errorf("Disk %s does not exist", i.GetTarget())
		return fmt.Errorf("disk %s does not exist", i.GetTarget())
	}

	disk, err := NewDisk(i.GetTarget(), WithLogger(config.Logger))
	if err != nil {
		return err
	}

	config.Logger.Infof("Partitioning device...")
	err = disk.NewPartitionTable(i.GetPartTable(), i.GetPartitions().PartitionsByInstallOrder(i.GetExtraPartitions()))
	if err != nil {
		config.Logger.Errorf("Failed creating new partition table: %s", err)
		return err
	}

	err = disk.ReReadPartitionTable()
	if err != nil {
		config.Logger.Errorf("Reread table: %s", err)
		return err
	}

	table, err := disk.GetPartitionTable()
	if err != nil {
		config.Logger.Errorf("table: %s", err)
		return err
	}
	err = disk.Close()
	if err != nil {
		config.Logger.Errorf("Close disk: %s", err)
	}
	// Sync changes
	syscall.Sync()
	// Trigger udevadm to refresh devices
	_, err = config.Runner.Run("udevadm", "trigger")
	if err != nil {
		config.Logger.Errorf("Udevadm trigger failed: %s", err)
	}
	_, err = config.Runner.Run("udevadm", "settle")
	if err != nil {
		config.Logger.Errorf("Udevadm settle failed: %s", err)
	}
	// Partitions are in order so we can format them via that
	for _, p := range table.GetPartitions() {
		for _, configPart := range i.GetPartitions().PartitionsByInstallOrder(i.GetExtraPartitions()) {
			if configPart.Name == cnst.BiosPartName {
				// Grub partition on non-EFI is not formatted. Grub is directly installed on it
				continue
			}
			// we have to match the Fs it was asked with the partition in the system
			if p.(*gpt.Partition).Name == configPart.Name {
				config.Logger.Debugf("Formatting partition: %s", configPart.FilesystemLabel)
				// Get full partition path by the /dev/disk/by-partlabel/ facility
				// So we don't need to infer the actual device under it but get udev to tell us
				// So this works for "normal" devices that have the "expected" partitions (i.e. /dev/sda has /dev/sda1, /dev/sda2)
				// And "weird" devices that have special subdevices like mmc or nvme
				// i.e. /dev/mmcblk0 has /dev/mmcblk0p1, /dev/mmcblk0p2
				dev, err := config.Fs.RawPath(fmt.Sprintf("/dev/disk/by-partlabel/%s", configPart.Name))
				if err != nil {
					return err
				}
				device, err := filepath.EvalSymlinks(dev)
				if err != nil {
					config.Logger.Errorf("Failed finding partition %s by partition label with symlink %s: %s", configPart.FilesystemLabel, dev, err)
				}
				err = FormatDevice(config.Logger, config.Runner, device, configPart.FS, configPart.FilesystemLabel)
				if err != nil {
					config.Logger.Errorf("Failed formatting partition: %s", err)
					return err
				}
				syscall.Sync()
			}
		}
	}
	return nil
}
