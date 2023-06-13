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
	"encoding/json"
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

type PartInfo struct {
	Name       string `json:"name,omitempty"`
	PkName     string `json:"pkname,omitempty"`
	Path       string `json:"path,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
	FsType     string `json:"fstype,omitempty"`
	Size       uint   `json:"size,omitempty"`
	Label      string `json:"label,omitempty"`
	RO         bool   `json:"ro,omitempty"`
}

// Lsblk is the struct to marshal the output of lsblk
type Lsblk struct {
	BlockDevices []PartInfo `json:"blockdevices,omitempty"`
}

// GetAllPartitions returns all partitions in the system for all disks
func GetAllPartitions(runner v1.Runner) (v1.PartitionList, error) {
	parts := v1.PartitionList{}

	// --list : show each partition only once
	// --bytes: don't show sizes in human readable format but rather number of bytes
	out, err := runner.Run("lsblk", "--list", "--bytes",
		"/dev/disk/by-path/*", "-o", "NAME,PKNAME,PATH,FSTYPE,MOUNTPOINT,SIZE,RO,LABEL", "-J")
	if err != nil {
		return parts, fmt.Errorf("lsblk failed with: %w\nOutput: %s", err, string(out))
	}

	return PartitionsFromLsblk(string(out))
}

func PartitionsFromLsblk(lsblkOutput string) (v1.PartitionList, error) {
	lsblk := &Lsblk{}
	parts := v1.PartitionList{}
	parentsLookup := map[string]string{}

	err := json.Unmarshal([]byte(lsblkOutput), lsblk)
	if err != nil {
		return parts, err
	}

	for _, p := range lsblk.BlockDevices {
		part := v1.Partition{}
		parentsLookup[p.Name] = p.PkName
		part.Name = p.Name
		part.FilesystemLabel = p.Label
		part.Size = p.Size / (1024 * 1024) // Converts B to MB
		part.Flags = nil
		part.MountPoint = p.Mountpoint
		part.FS = p.FsType
		part.Path = p.Path
		parts = append(parts, &part)
	}

	// Add `Disk` field to all partitions
	for _, p := range parts {
		disk := findDisk(parentsLookup, p.Name)
		if disk != "" {
			p.Disk = filepath.Join("/dev", disk)
		}
	}

	return parts, nil
}

func findDisk(parentsLookup map[string]string, pName string) string {
	parent, hasParent := parentsLookup[pName]
	if !hasParent || parent == "" {
		return pName // A disk's disk is itself
	}

	return findDisk(parentsLookup, parent)
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
