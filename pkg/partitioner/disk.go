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

package partitioner

import (
	"errors"
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"os"
	"regexp"
	"strings"
	"time"

	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/twpayne/go-vfs"
)

const (
	partitionTries = 10
	// Parted warning substring for expanded disks without fixing GPT headers
	partedWarn = "Not all of the space available"
)

var unallocatedRegexp = regexp.MustCompile(partedWarn)

type Disk struct {
	device  string
	sectorS uint
	lastS   uint
	parts   []Partition
	label   string
	runner  v1.Runner
	fs      v1.FS
	logger  v1.Logger
}

// MiBToSectors returns the number of sectors that correspond to the given amount
// of MB.
func MiBToSectors(totalMB uint, sectorSize uint) uint {
	bytes := totalMB * 1024 * 1024
	return bytes / sectorSize
}

// SectorsToMiB returns the number of MBs that correspond to the given amount
// of sectors.
func SectorsToMiB(totalSectors uint, sectorSize uint) uint {
	bytes := totalSectors * sectorSize

	return bytes / (1024 * 1024) // Mb
}

func NewDisk(device string, opts ...DiskOptions) *Disk {
	dev := &Disk{device: device}

	for _, opt := range opts {
		if err := opt(dev); err != nil {
			return nil
		}
	}

	if dev.runner == nil {
		dev.runner = &v1.RealRunner{}
	}

	if dev.fs == nil {
		dev.fs = vfs.OSFS
	}

	if dev.logger == nil {
		dev.logger = v1.NewLogger()
	}

	return dev
}

// FormatDevice formats a block device with the given parameters
func FormatDevice(runner v1.Runner, device string, fileSystem string, label string, opts ...string) error {
	mkfs := MkfsCall{fileSystem: fileSystem, label: label, customOpts: opts, dev: device, runner: runner}
	_, err := mkfs.Apply()
	return err
}

func (dev Disk) String() string {
	return dev.device
}

func (dev Disk) GetSectorSize() uint {
	return dev.sectorS
}

func (dev Disk) GetLastSector() uint {
	return dev.lastS
}

func (dev Disk) GetLabel() string {
	return dev.label
}

func (dev *Disk) Exists() bool {
	fi, err := dev.fs.Stat(dev.device)
	if err != nil {
		return false
	}
	// resolve symlink if any
	if fi.Mode()&os.ModeSymlink != 0 {
		d, err := dev.fs.Readlink(dev.device)
		if err != nil {
			return false
		}
		dev.device = d
	}
	return true
}

func (dev *Disk) Reload() error {
	pc := NewPartedCall(dev.String(), dev.runner)
	prnt, err := pc.Print()
	if err != nil {
		return fmt.Errorf("error: %w. output: %s", err, prnt)
	}

	// if the unallocated space warning is found it is assumed GPT headers
	// are not properly located to match disk size, so we use sgdisk
	// to expand the partition table to fully match disk size.
	// It is expected that in upcoming parted releases (>3.4) there will be
	// --fix flag to solve this issue transparently on the fly on any parted call.
	// However this option is not yet present in all major distros.
	if unallocatedRegexp.Match([]byte(prnt)) {
		// Parted has not a proper way to doing it in non interactive mode,
		// because of that we use sgdisk for that...
		out, err := dev.runner.Run("sgdisk", "-e", dev.device)
		if err != nil {
			return fmt.Errorf("error: %w. output: %s", err, string(out))
		}
		// Reload disk data with fixed headers
		prnt, err = pc.Print()
		if err != nil {
			return fmt.Errorf("error: %w. output: %s", err, prnt)
		}
	}

	sectorS, err := pc.GetSectorSize(prnt)
	if err != nil {
		return err
	}
	lastS, err := pc.GetLastSector(prnt)
	if err != nil {
		return err
	}
	label, err := pc.GetPartitionTableLabel(prnt)
	if err != nil {
		return err
	}
	partitions := pc.GetPartitions(prnt)
	dev.sectorS = sectorS
	dev.lastS = lastS
	dev.parts = partitions
	dev.label = label
	return nil
}

// Size is expressed in MiB here
func (dev *Disk) CheckDiskFreeSpaceMiB(minSpace uint) bool {
	freeS, err := dev.GetFreeSpace()
	if err != nil {
		dev.logger.Warnf("Could not calculate disk free space")
		return false
	}
	minSec := MiBToSectors(minSpace, dev.sectorS)

	return freeS >= minSec
}

func (dev *Disk) GetFreeSpace() (uint, error) {
	//Check we have loaded partition table data
	if dev.sectorS == 0 {
		err := dev.Reload()
		if err != nil {
			dev.logger.Errorf("Failed analyzing disk: %v\n", err)
			return 0, err
		}
	}

	return dev.computeFreeSpace(), nil
}

func (dev Disk) computeFreeSpace() uint {
	if len(dev.parts) > 0 {
		lastPart := dev.parts[len(dev.parts)-1]
		return dev.lastS - (lastPart.StartS + lastPart.SizeS - 1)
	}
	// First partition starts at a 1MiB offset
	return dev.lastS - (1*1024*1024/dev.sectorS - 1)
}

func (dev Disk) computeFreeSpaceWithoutLast() uint {
	if len(dev.parts) > 1 {
		part := dev.parts[len(dev.parts)-2]
		return dev.lastS - (part.StartS + part.SizeS - 1)
	}
	// Assume first partitions is alined to 1MiB
	return dev.lastS - (1024*1024/dev.sectorS - 1)
}

func (dev *Disk) NewPartitionTable(label string) (string, error) {
	match, _ := regexp.MatchString("msdos|gpt", label)
	if !match {
		return "", errors.New("Invalid partition table type, only msdos and gpt are supported")
	}
	pc := NewPartedCall(dev.String(), dev.runner)
	pc.SetPartitionTableLabel(label)
	pc.WipeTable(true)
	out, err := pc.WriteChanges()
	if err != nil {
		return out, err
	}
	err = dev.Reload()
	if err != nil {
		dev.logger.Errorf("Failed analyzing disk: %v\n", err)
		return "", err
	}
	return out, nil
}

// AddPartition adds a partition. Size is expressed in MiB here
// Size is expressed in MiB here
func (dev *Disk) AddPartition(size uint, fileSystem string, pLabel string, flags ...string) (int, error) {
	pc := NewPartedCall(dev.String(), dev.runner)

	//Check we have loaded partition table data
	if dev.sectorS == 0 {
		err := dev.Reload()
		if err != nil {
			dev.logger.Errorf("Failed analyzing disk: %v\n", err)
			return 0, err
		}
	}

	pc.SetPartitionTableLabel(dev.label)

	var partNum int
	var startS uint
	if len(dev.parts) > 0 {
		lastP := len(dev.parts) - 1
		partNum = dev.parts[lastP].Number
		startS = dev.parts[lastP].StartS + dev.parts[lastP].SizeS
	} else {
		//First partition is aligned at 1MiB
		startS = 1024 * 1024 / dev.sectorS
	}

	size = MiBToSectors(size, dev.sectorS)
	freeS := dev.computeFreeSpace()
	if size > freeS {
		return 0, fmt.Errorf("not enough free space in disk. Required: %d sectors; Available %d sectors", size, freeS)
	}

	partNum++
	var part = Partition{
		Number:     partNum,
		StartS:     startS,
		SizeS:      size,
		PLabel:     pLabel,
		FileSystem: fileSystem,
	}

	pc.CreatePartition(&part)
	for _, flag := range flags {
		pc.SetPartitionFlag(partNum, flag, true)
	}

	out, err := pc.WriteChanges()
	dev.logger.Debugf("partitioner output: %s", out)
	if err != nil {
		dev.logger.Errorf("Failed creating partition: %v", err)
		return 0, err
	}

	// Reload new partition in dev
	err = dev.Reload()
	if err != nil {
		dev.logger.Errorf("Failed analyzing disk: %v\n", err)
		return 0, err
	}
	return partNum, nil
}

func (dev Disk) FormatPartition(partNum int, fileSystem string, label string) (string, error) {
	pDev, err := dev.FindPartitionDevice(partNum)
	if err != nil {
		return "", err
	}

	mkfs := MkfsCall{fileSystem: fileSystem, label: label, customOpts: []string{}, dev: pDev, runner: dev.runner}
	return mkfs.Apply()
}

func (dev Disk) WipeFsOnPartition(device string) error {
	_, err := dev.runner.Run("wipefs", "--all", device)
	return err
}

func (dev Disk) FindPartitionDevice(partNum int) (string, error) {
	re := regexp.MustCompile(`.*\d+$`)
	var device string

	if match := re.Match([]byte(dev.device)); match {
		device = fmt.Sprintf("%sp%d", dev.device, partNum)
	} else {
		device = fmt.Sprintf("%s%d", dev.device, partNum)
	}

	for tries := 0; tries <= partitionTries; tries++ {
		dev.logger.Debugf("Trying to find the partition device %d of device %s (try number %d)", partNum, dev, tries+1)
		_, _ = dev.runner.Run("udevadm", "settle")
		if exists, _ := fsutils.Exists(dev.fs, device); exists {
			return device, nil
		}
		time.Sleep(1 * time.Second)
	}
	return "", fmt.Errorf("could not find partition device '%s' for partition %d", device, partNum)
}

// ExpandLastPartition expands the latest partition in the disk. Size is expressed in MiB here
// Size is expressed in MiB here
func (dev *Disk) ExpandLastPartition(size uint) (string, error) {
	pc := NewPartedCall(dev.String(), dev.runner)

	//Check we have loaded partition table data
	if dev.sectorS == 0 {
		err := dev.Reload()
		if err != nil {
			dev.logger.Errorf("Failed analyzing disk: %v\n", err)
			return "", err
		}
	}

	pc.SetPartitionTableLabel(dev.label)

	if len(dev.parts) == 0 {
		return "", errors.New("There is no partition to expand")
	}

	// This is a safe guard to avoid re-executing this operation on each boot.
	// On a system which is partitioned, one sector is left out, so we
	// use a buffer to avoid expansion at all if we don't have at least 10MB free.
	// There is no point to try to expand a partition to get 10MB - if we do expansion is to take over
	// the space for the last partition, which is meant for the persistent data (and thus we want to expand for gaining GBs, not MBs.)
	freeSpace := dev.computeFreeSpace()
	limit := MiBToSectors(10, dev.sectorS)
	if freeSpace < limit {
		return "", fmt.Errorf("not enough free space (%d) to expand, at least %d is required", freeSpace, limit)
	}

	part := dev.parts[len(dev.parts)-1]
	var sizeSectors uint
	if size > 0 {
		sizeSectors = MiBToSectors(size, dev.sectorS)
		part := dev.parts[len(dev.parts)-1]
		if sizeSectors < part.SizeS {
			return "", errors.New("Layout plugin can only expand a partition, not shrink it")
		}
		freeS := dev.computeFreeSpaceWithoutLast()
		if sizeSectors > freeS {
			return "", fmt.Errorf("not enough free space for to expand last partition up to %d sectors", size)
		}
	}
	part.SizeS = sizeSectors
	pc.DeletePartition(part.Number)
	pc.CreatePartition(&part)
	out, err := pc.WriteChanges()
	if err != nil {
		return out, err
	}
	err = dev.Reload()
	if err != nil {
		return "", err
	}
	pDev, err := dev.FindPartitionDevice(part.Number)
	if err != nil {
		return "", err
	}
	return dev.expandFilesystem(pDev)
}

func (dev Disk) expandFilesystem(device string) (string, error) {
	var out []byte
	var err error

	fs, err := partitions.GetPartitionFS(device)
	if err != nil {
		return fs, err
	}

	switch strings.TrimSpace(fs) {
	case "ext2", "ext3", "ext4":
		out, err = dev.runner.Run("e2fsck", "-fy", device)
		if err != nil {
			return string(out), err
		}
		out, err = dev.runner.Run("resize2fs", device)

		if err != nil {
			return string(out), err
		}
	case "xfs":
		// to grow an xfs fs it needs to be mounted :/
		tmpDir, err := fsutils.TempDir(dev.fs, "", "partitioner")
		defer func(fs v1.FS, path string) {
			_ = fs.RemoveAll(path)
		}(dev.fs, tmpDir)

		if err != nil {
			return string(out), err
		}
		out, err = dev.runner.Run("mount", "-t", "xfs", device, tmpDir)
		if err != nil {
			return string(out), err
		}
		_, err = dev.runner.Run("xfs_growfs", tmpDir)
		if err != nil {
			// If we error out, try to umount the dir to not leave it hanging
			out, err2 := dev.runner.Run("umount", tmpDir)
			if err2 != nil {
				return string(out), err2
			}
			return string(out), err
		}
		out, err = dev.runner.Run("umount", tmpDir)
		if err != nil {
			return string(out), err
		}
	default:
		return "", fmt.Errorf("could not find filesystem for %s, not resizing the filesystem", device)
	}

	return "", nil
}
