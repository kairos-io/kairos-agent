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

package partitions

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/kairos-io/kairos-sdk/types"
	log "github.com/sirupsen/logrus"
)

// GetAllPartitions returns all partitions in the system for all disks
func GetAllPartitions(logger *types.KairosLogger) (types.PartitionList, error) {
	var parts []*types.Partition

	for _, d := range ghw.GetDisks(ghw.NewPaths(""), logger) {
		for _, part := range d.Partitions {
			parts = append(parts, part)
		}
	}
	return parts, nil
}

// GetMountPointByLabel will try to get the mountpoint by using the label only
// so we can identify mounts the have been mounted with /dev/disk/by-label stanzas
func GetMountPointByLabel(label string) string {
	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	var r io.ReadCloser
	r, err := os.Open("/proc/mounts")
	if err != nil {
		return ""
	}
	defer r.Close()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		partition, mountpoint := parseMountEntry(line)
		if partition == fmt.Sprintf("/dev/disk/by-label/%s", label) {
			return mountpoint
		}
	}
	return ""
}

func parseMountEntry(line string) (string, string) {
	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	if line[0] != '/' {
		return "", ""
	}
	fields := strings.Fields(line)

	if len(fields) < 4 {
		return "", ""
	}

	// We do some special parsing of the mountpoint, which may contain space,
	// tab and newline characters, encoded into the mount entry line using their
	// octal-to-string representations. From the GNU mtab man pages:
	//
	//   "Therefore these characters are encoded in the files and the getmntent
	//   function takes care of the decoding while reading the entries back in.
	//   '\040' is used to encode a space character, '\011' to encode a tab
	//   character, '\012' to encode a newline character, and '\\' to encode a
	//   backslash."
	mp := fields[1]
	r := strings.NewReplacer(
		"\\011", "\t", "\\012", "\n", "\\040", " ", "\\\\", "\\",
	)
	mp = r.Replace(mp)

	return fields[0], mp
}

// GetPartitionViaDM tries to get the partition via devicemapper for reset
// We only need to get all this info due to the fS that we need to use to format the partition
// Otherwise we could just format with the label ¯\_(ツ)_/¯
// TODO: store info about persistent and oem in the state.yaml so we can directly load it
func GetPartitionViaDM(fs v1.FS, label string) *types.Partition {
	var part *types.Partition
	rootPath, _ := fs.RawPath("/")
	lp := ghw.NewPaths(rootPath)

	devices, _ := fs.ReadDir(lp.SysBlock)
	for _, dev := range devices {
		if !strings.HasPrefix(dev.Name(), "dm-") {
			continue
		}
		// read dev number
		devNo, err := fs.ReadFile(filepath.Join(lp.SysBlock, dev.Name(), "dev"))
		// No device number, empty dm?
		if err != nil || string(devNo) == "" {
			continue
		}
		udevID := "b" + strings.TrimSpace(string(devNo))
		// Read udev info about this device
		udevBytes, _ := fs.ReadFile(filepath.Join(lp.RunUdevData, udevID))
		udevInfo := make(map[string]string)
		for _, udevLine := range strings.Split(string(udevBytes), "\n") {
			if strings.HasPrefix(udevLine, "E:") {
				if s := strings.SplitN(udevLine[2:], "=", 2); len(s) == 2 {
					udevInfo[s[0]] = s[1]
					continue
				}
			}
		}
		if udevInfo["ID_FS_LABEL"] == label {
			// Found it!
			partitionFS := udevInfo["ID_FS_TYPE"]
			partitionName := udevInfo["DM_LV_NAME"]

			part = &types.Partition{
				Name:            partitionName,
				FilesystemLabel: label,
				FS:              partitionFS,
				Path:            filepath.Join("/dev/disk/by-label/", label),
			}
			// Read size
			sizeInSectors, err1 := fs.ReadFile(filepath.Join(lp.SysBlock, dev.Name(), "size"))
			sectorSize, err2 := fs.ReadFile(filepath.Join(lp.SysBlock, dev.Name(), "queue", "logical_block_size"))
			if err1 == nil && err2 == nil {
				sizeInSectorsInt, err1 := strconv.Atoi(strings.TrimSpace(string(sizeInSectors)))
				sectorSizeInt, err2 := strconv.Atoi(strings.TrimSpace(string(sectorSize)))
				if err1 == nil && err2 == nil {
					// Multiply size in sectors by sector size
					// Although according to the source this will always be 512: https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/tree/include/linux/types.h?#n120
					finalSize := sizeInSectorsInt * sectorSizeInt
					part.Size = uint(finalSize)
				}
			}

			// Read slaves to get the device
			slaves, err := fs.ReadDir(filepath.Join(lp.SysBlock, dev.Name(), "slaves"))
			if err != nil {
				log.Debugf("Error getting slaves for %s\n", filepath.Join(lp.SysBlock, dev.Name()))
			}
			if len(slaves) == 1 {
				// We got the partition this dm is associated to, now lets read that partition udev identifier
				partNumber, err := fs.ReadFile(filepath.Join(lp.SysBlock, dev.Name(), "slaves", slaves[0].Name(), "dev"))
				// If no errors and partNumber not empty read the device from udev
				if err == nil || string(partNumber) != "" {
					// Now for some magic!
					// If udev partition identifier is bXXX:5 then the parent disk should be on bXXX:0
					// At least for block devices that seems to be the pattern
					// So let's get just the first part of the id and append a 0 at the end
					// If we wanted to make this safer we could read the udev data of the partNumber and
					// extract the udevInfo called ID_PART_ENTRY_DISK which gives us the udev ID of the parent disk
					baseID := strings.Split(strings.TrimSpace(string(partNumber)), ":")
					udevID = fmt.Sprintf("b%s:0", baseID[0])
					// Read udev info about this device
					udevBytes, _ = fs.ReadFile(filepath.Join(lp.RunUdevData, udevID))
					udevInfo = make(map[string]string)
					for _, udevLine := range strings.Split(string(udevBytes), "\n") {
						if strings.HasPrefix(udevLine, "E:") {
							if s := strings.SplitN(udevLine[2:], "=", 2); len(s) == 2 {
								udevInfo[s[0]] = s[1]
								continue
							}
						}
					}
					// Read the disk path.
					// This is the only decent info that udev provides in this case that we can use to identify the device :/
					diskPath := udevInfo["ID_PATH"]
					// Read the full path to the disk using the path
					parentDiskFullPath, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-path/", diskPath))
					if err == nil {
						part.Disk = parentDiskFullPath
					}
				}

			}

			// Read /proc/mounts to get the mountpoint if any
			// We need the disk to be filled to get the mountpoint
			if part.Disk != "" {
				mounts, err := fs.ReadFile("/proc/mounts")
				if err == nil {
					for _, l := range strings.Split(string(mounts), "\n") {
						entry := strings.Split(l, " ")
						// entry is `device mountpoint fstype options unused unused`
						// The unused fields are there for compatibility with mtab
						if len(entry) > 1 {
							// Check against the path as lvm devices are not mounted against /dev, they are mounted via label
							if entry[0] == part.Path {
								part.MountPoint = entry[1]
								break
							}
						}
					}
				}
			}

			return part
		}
	}
	return part
}

// GetEfiPartition returns the EFI partition by looking for the partition with the label "COS_GRUB"
func GetEfiPartition(logger *types.KairosLogger) (*types.Partition, error) {
	var efiPartition *types.Partition
	for _, d := range ghw.GetDisks(ghw.NewPaths(""), logger) {
		for _, part := range d.Partitions {
			if part.FilesystemLabel == constants.EfiLabel {
				efiPartition = part
				break
			}
		}
	}

	if efiPartition == nil {
		return efiPartition, fmt.Errorf("could not find EFI partition")
	}
	return efiPartition, nil
}
