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

package utils

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/context"
	"github.com/jaypipes/ghw/pkg/linuxpath"
	ghwUtil "github.com/jaypipes/ghw/pkg/util"
	cnst "github.com/kairos-io/kairos/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
)

// ghwPartitionToInternalPartition transforms a block.Partition from ghw lib to our v1.Partition type
func ghwPartitionToInternalPartition(partition *block.Partition) *v1.Partition {
	return &v1.Partition{
		FilesystemLabel: partition.FilesystemLabel,
		Size:            uint(partition.SizeBytes / (1024 * 1024)), // Converts B to MB
		Name:            partition.Name,
		FS:              partition.Type,
		Flags:           nil,
		MountPoint:      partition.MountPoint,
		Path:            filepath.Join("/dev", partition.Name),
		Disk:            filepath.Join("/dev", partition.Disk.Name),
	}
}

// GetAllPartitions returns all partitions in the system for all disks
func GetAllPartitions() (v1.PartitionList, error) {
	var parts []*v1.Partition
	blockDevices, err := block.New(ghw.WithDisableTools(), ghw.WithDisableWarnings())
	if err != nil {
		return nil, err
	}
	for _, d := range blockDevices.Disks {
		for _, part := range d.Partitions {
			parts = append(parts, ghwPartitionToInternalPartition(part))
		}
	}
	return parts, nil
}

// GetPartitionFS gets the FS of a partition given
func GetPartitionFS(partition string) (string, error) {
	// We want to have the device always prefixed with a /dev
	if !strings.HasPrefix(partition, "/dev") {
		partition = filepath.Join("/dev", partition)
	}
	blockDevices, err := block.New(ghw.WithDisableTools(), ghw.WithDisableWarnings())
	if err != nil {
		return "", err
	}

	for _, disk := range blockDevices.Disks {
		for _, part := range disk.Partitions {
			if filepath.Join("/dev", part.Name) == partition {
				if part.Type == ghwUtil.UNKNOWN {
					return "", fmt.Errorf("could not find filesystem for partition %s", partition)
				}
				return part.Type, nil
			}
		}
	}
	return "", fmt.Errorf("could not find filesystem for partition %s", partition)
}

// GetPersistentViaDM tries to get the persistent partition via devicemapper for reset
// We only need to get all this info due to the fS that we need to use to format the partition
// Otherwise we could just format with the label ¯\_(ツ)_/¯
// TODO: store info about persistent and oem in the state.yaml so we can directly load it
func GetPersistentViaDM(fs v1.FS) *v1.Partition {
	var persistent *v1.Partition
	rootPath, _ := fs.RawPath("/")
	ctx := context.New(ghw.WithDisableTools(), ghw.WithDisableWarnings(), ghw.WithChroot(rootPath))
	lp := linuxpath.New(ctx)
	devices, _ := fs.ReadDir(lp.SysBlock)
	for _, dev := range devices {
		if !strings.HasPrefix(dev.Name(), "dm-") {
			continue
		}
		// read dev number
		devNo, err := fs.ReadFile(filepath.Join(lp.SysBlock, dev.Name(), "dev"))
		// No slaves, empty dm?
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
		if udevInfo["ID_FS_LABEL"] == cnst.PersistentLabel {
			// Found it!
			persistentFS := udevInfo["ID_FS_TYPE"]
			return &v1.Partition{
				Name:            cnst.PersistentPartName,
				FilesystemLabel: cnst.PersistentLabel,
				FS:              persistentFS,
				Path:            filepath.Join("/dev/disk/by-label/", cnst.PersistentLabel),
			}
		}
	}
	return persistent
}
