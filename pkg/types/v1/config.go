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

package v1

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"gopkg.in/yaml.v3"
)

const (
	GPT   = "gpt"
	BIOS  = "bios"
	MSDOS = "msdos"
	EFI   = "efi"
	esp   = "esp"
	bios  = "bios_grub"
	boot  = "boot"
)

type Spec interface {
	Sanitize() error
	ShouldReboot() bool
	ShouldShutdown() bool
}

// SharedInstallSpec is the interface that Install specs need to implement
type SharedInstallSpec interface {
	GetPartTable() string
	GetTarget() string
	GetPartitions() ElementalPartitions
	GetExtraPartitions() PartitionList
}

// InstallSpec struct represents all the installation action details
type InstallSpec struct {
	Target          string              `yaml:"device,omitempty" mapstructure:"device"`
	Firmware        string              `yaml:"firmware,omitempty" mapstructure:"firmware"`
	PartTable       string              `yaml:"part-table,omitempty" mapstructure:"part-table"`
	Partitions      ElementalPartitions `yaml:"partitions,omitempty" mapstructure:"partitions"`
	ExtraPartitions PartitionList       `yaml:"extra-partitions,omitempty" mapstructure:"extra-partitions"`
	NoFormat        bool                `yaml:"no-format,omitempty" mapstructure:"no-format"`
	Force           bool                `yaml:"force,omitempty" mapstructure:"force"`
	CloudInit       []string            `yaml:"cloud-init,omitempty" mapstructure:"cloud-init"`
	Iso             string              `yaml:"iso,omitempty" mapstructure:"iso"`
	GrubDefEntry    string              `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	Tty             string              `yaml:"tty,omitempty" mapstructure:"tty"`
	Reboot          bool                `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff        bool                `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	ExtraDirsRootfs []string            `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs"`
	Active          Image               `yaml:"system,omitempty" mapstructure:"system"`
	Recovery        Image               `yaml:"recovery-system,omitempty" mapstructure:"recovery-system"`
	Passive         Image
	GrubConf        string
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (i *InstallSpec) Sanitize() error {
	if i.Active.Source.IsEmpty() && i.Iso == "" {
		return fmt.Errorf("undefined system source to install")
	}
	if i.Partitions.State == nil || i.Partitions.State.MountPoint == "" {
		return fmt.Errorf("undefined state partition")
	}
	// Set the image file name depending on the filesystem
	recoveryMnt := constants.RecoveryDir
	if i.Partitions.Recovery != nil && i.Partitions.Recovery.MountPoint != "" {
		recoveryMnt = i.Partitions.Recovery.MountPoint
	}
	if i.Recovery.FS == constants.SquashFs {
		i.Recovery.File = filepath.Join(recoveryMnt, "cOS", constants.RecoverySquashFile)
	} else {
		i.Recovery.File = filepath.Join(recoveryMnt, "cOS", constants.RecoveryImgFile)
	}

	// Check for extra partitions having set its size to 0
	extraPartsSizeCheck := 0
	for _, p := range i.ExtraPartitions {
		if p.Size == 0 {
			extraPartsSizeCheck++
		}
	}

	if extraPartsSizeCheck > 1 {
		return fmt.Errorf("more than one extra partition has its size set to 0. Only one partition can have its size set to 0 which means that it will take all the available disk space in the device")
	}
	// Check for both an extra partition and the persistent partition having size set to 0
	if extraPartsSizeCheck == 1 && i.Partitions.Persistent.Size == 0 {
		return fmt.Errorf("both persistent partition and extra partitions have size set to 0. Only one partition can have its size set to 0 which means that it will take all the available disk space in the device")
	}

	// Set default labels in case the config from cloud/config overrides this values.
	// we need them to be on fixed values, otherwise we wont know where to find things on boot, on reset, etc...
	i.Partitions.SetDefaultLabels()

	return i.Partitions.SetFirmwarePartitions(i.Firmware, i.PartTable)
}

func (i *InstallSpec) ShouldReboot() bool                 { return i.Reboot }
func (i *InstallSpec) ShouldShutdown() bool               { return i.PowerOff }
func (i *InstallSpec) GetTarget() string                  { return i.Target }
func (i *InstallSpec) GetPartTable() string               { return i.PartTable }
func (i *InstallSpec) GetPartitions() ElementalPartitions { return i.Partitions }
func (i *InstallSpec) GetExtraPartitions() PartitionList  { return i.ExtraPartitions }

// ResetSpec struct represents all the reset action details
type ResetSpec struct {
	FormatPersistent bool     `yaml:"reset-persistent,omitempty" mapstructure:"reset-persistent"`
	FormatOEM        bool     `yaml:"reset-oem,omitempty" mapstructure:"reset-oem"`
	Reboot           bool     `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff         bool     `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	GrubDefEntry     string   `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	Tty              string   `yaml:"tty,omitempty" mapstructure:"tty"`
	ExtraDirsRootfs  []string `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs"`
	Active           Image    `yaml:"system,omitempty" mapstructure:"system"`
	Passive          Image
	Partitions       ElementalPartitions
	Target           string
	Efi              bool
	GrubConf         string
	State            *InstallState
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (r *ResetSpec) Sanitize() error {
	if r.Active.Source.IsEmpty() {
		return fmt.Errorf("undefined system source to reset to")
	}
	if r.Partitions.State == nil || r.Partitions.State.MountPoint == "" {
		return fmt.Errorf("undefined state partition")
	}
	return nil
}

func (r *ResetSpec) ShouldReboot() bool   { return r.Reboot }
func (r *ResetSpec) ShouldShutdown() bool { return r.PowerOff }

type UpgradeSpec struct {
	RecoveryUpgrade bool     `yaml:"recovery,omitempty" mapstructure:"recovery"`
	Active          Image    `yaml:"system,omitempty" mapstructure:"system"`
	Recovery        Image    `yaml:"recovery-system,omitempty" mapstructure:"recovery-system"`
	GrubDefEntry    string   `yaml:"grub-entry-name,omitempty" mapstructure:"grub-entry-name"`
	Reboot          bool     `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff        bool     `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	ExtraDirsRootfs []string `yaml:"extra-dirs-rootfs,omitempty" mapstructure:"extra-dirs-rootfs"`
	Passive         Image
	Partitions      ElementalPartitions
	State           *InstallState
}

// Sanitize checks the consistency of the struct, returns error
// if unsolvable inconsistencies are found
func (u *UpgradeSpec) Sanitize() error {
	// Dont sanitize on a qcs6490 board, we dont have any normal partitions
	// Check is here to avoid circular deps
	if _, err := os.Stat("/build.prop"); err == nil {
		return nil
	}
	if u.RecoveryUpgrade {
		if u.Recovery.Source.IsEmpty() {
			return fmt.Errorf("undefined upgrade source")
		}
		if u.Partitions.Recovery == nil || u.Partitions.Recovery.MountPoint == "" {
			return fmt.Errorf("undefined recovery partition")
		}
	} else {
		if u.Active.Source.IsEmpty() {
			return fmt.Errorf("undefined upgrade source")
		}
		if u.Partitions.State == nil || u.Partitions.State.MountPoint == "" {
			return fmt.Errorf("undefined state partition")
		}
	}
	return nil
}

func (u *UpgradeSpec) ShouldReboot() bool   { return u.Reboot }
func (u *UpgradeSpec) ShouldShutdown() bool { return u.PowerOff }

// EmptySpec is an empty spec for places that may need to inject a spec but doent really have one associated like firstboot
type EmptySpec struct {
}

func (r *EmptySpec) Sanitize() error {
	return nil
}

func (r *EmptySpec) ShouldReboot() bool   { return false }
func (r *EmptySpec) ShouldShutdown() bool { return false }

// Partition struct represents a partition with its commonly configurable values, size in MiB
type Partition struct {
	Name            string
	FilesystemLabel string   `yaml:"label,omitempty" mapstructure:"label"`
	Size            uint     `yaml:"size,omitempty" mapstructure:"size"`
	FS              string   `yaml:"fs,omitempty" mapstrcuture:"fs"`
	Flags           []string `yaml:"flags,omitempty" mapstrcuture:"flags"`
	Label           string
	MountPoint      string
	Path            string
	Disk            string
}

type PartitionList []*Partition

// GetByName gets a partitions by its name from the PartitionList
func (pl PartitionList) GetByName(name string) *Partition {
	var part *Partition

	for _, p := range pl {
		if p.Name == name {
			part = p
			if part.MountPoint != "" {
				return part
			}
		}
	}
	return part
}

// GetByLabel gets a partition by its label from the PartitionList
func (pl PartitionList) GetByLabel(label string) *Partition {
	var part *Partition

	for _, p := range pl {
		if p.FilesystemLabel == label {
			part = p
			if part.MountPoint != "" {
				return part
			}
		}
	}
	return part
}

type ElementalPartitions struct {
	BIOS       *Partition
	EFI        *Partition
	OEM        *Partition `yaml:"oem,omitempty" mapstructure:"oem"`
	Recovery   *Partition `yaml:"recovery,omitempty" mapstructure:"recovery"`
	State      *Partition `yaml:"state,omitempty" mapstructure:"state"`
	Persistent *Partition `yaml:"persistent,omitempty" mapstructure:"persistent"`
}

// SetFirmwarePartitions sets firmware partitions for a given firmware and partition table type
func (ep *ElementalPartitions) SetFirmwarePartitions(firmware string, partTable string) error {
	if firmware == EFI && partTable == GPT {
		ep.EFI = &Partition{
			FilesystemLabel: constants.EfiLabel,
			Size:            constants.EfiSize,
			Name:            constants.EfiPartName,
			FS:              constants.EfiFs,
			MountPoint:      constants.EfiDir,
			Flags:           []string{esp},
		}
		ep.BIOS = nil
	} else if firmware == BIOS && partTable == GPT {
		ep.BIOS = &Partition{
			FilesystemLabel: "",
			Size:            constants.BiosSize,
			Name:            constants.BiosPartName,
			FS:              "",
			MountPoint:      "",
			Flags:           []string{bios},
		}
		ep.EFI = nil
	} else {
		if ep.State == nil {
			return fmt.Errorf("nil state partition")
		}
		ep.State.Flags = []string{boot}
		ep.EFI = nil
		ep.BIOS = nil
	}
	return nil
}

// SetDefaultLabels sets the default labels for oem, state, persistent and recovery partitions.
func (ep *ElementalPartitions) SetDefaultLabels() {
	ep.OEM.FilesystemLabel = constants.OEMLabel
	ep.OEM.Name = constants.OEMPartName
	ep.State.FilesystemLabel = constants.StateLabel
	ep.State.Name = constants.StatePartName
	ep.Persistent.FilesystemLabel = constants.PersistentLabel
	ep.Persistent.Name = constants.PersistentPartName
	ep.Recovery.FilesystemLabel = constants.RecoveryLabel
	ep.Recovery.Name = constants.RecoveryPartName
}

// NewElementalPartitionsFromList fills an ElementalPartitions instance from given
// partitions list. First tries to match partitions by partition label, if not,
// it tries to match partitions by default filesystem label
// TODO find a way to map custom labels when partition labels are not available
func NewElementalPartitionsFromList(pl PartitionList) ElementalPartitions {
	ep := ElementalPartitions{}
	ep.BIOS = pl.GetByName(constants.BiosPartName)
	ep.EFI = pl.GetByName(constants.EfiPartName)
	if ep.EFI == nil {
		ep.EFI = pl.GetByLabel(constants.EfiLabel)
	}
	ep.OEM = pl.GetByName(constants.OEMPartName)
	if ep.OEM == nil {
		ep.OEM = pl.GetByLabel(constants.OEMLabel)
	}
	ep.Recovery = pl.GetByName(constants.RecoveryPartName)
	if ep.Recovery == nil {
		ep.Recovery = pl.GetByLabel(constants.RecoveryLabel)
	}
	ep.State = pl.GetByName(constants.StatePartName)
	if ep.State == nil {
		ep.State = pl.GetByLabel(constants.StateLabel)
	}
	ep.Persistent = pl.GetByName(constants.PersistentPartName)
	if ep.Persistent == nil {
		ep.Persistent = pl.GetByLabel(constants.PersistentLabel)
	}
	return ep
}

// PartitionsByInstallOrder sorts partitions according to the default layout
// nil partitions are ignored
// partition with 0 size is set last
func (ep ElementalPartitions) PartitionsByInstallOrder(extraPartitions PartitionList, excludes ...*Partition) PartitionList {
	partitions := PartitionList{}
	var lastPartition *Partition

	inExcludes := func(part *Partition, list ...*Partition) bool {
		for _, p := range list {
			if part == p {
				return true
			}
		}
		return false
	}

	if ep.BIOS != nil && !inExcludes(ep.BIOS, excludes...) {
		partitions = append(partitions, ep.BIOS)
	}
	if ep.EFI != nil && !inExcludes(ep.EFI, excludes...) {
		partitions = append(partitions, ep.EFI)
	}
	if ep.OEM != nil && !inExcludes(ep.OEM, excludes...) {
		partitions = append(partitions, ep.OEM)
	}
	if ep.Recovery != nil && !inExcludes(ep.Recovery, excludes...) {
		partitions = append(partitions, ep.Recovery)
	}
	if ep.State != nil && !inExcludes(ep.State, excludes...) {
		partitions = append(partitions, ep.State)
	}
	if ep.Persistent != nil && !inExcludes(ep.Persistent, excludes...) {
		// Check if we have to set this partition the latest due size == 0
		if ep.Persistent.Size == 0 {
			lastPartition = ep.Persistent
		} else {
			partitions = append(partitions, ep.Persistent)
		}
	}
	for _, p := range extraPartitions {
		// Check if we have to set this partition the latest due size == 0
		// Also check that we didn't set already the persistent to last in which case ignore this
		// InstallConfig.Sanitize should have already taken care of failing if this is the case, so this is extra protection
		if p.Size == 0 {
			if lastPartition != nil {
				// Ignore this part, we are not setting 2 parts to have 0 size!
				continue
			}
			lastPartition = p
		} else {
			partitions = append(partitions, p)
		}
	}

	// Set the last partition in the list the partition which has 0 size, so it grows to use the rest of free space
	if lastPartition != nil {
		partitions = append(partitions, lastPartition)
	}

	return partitions
}

// PartitionsByMountPoint sorts partitions according to its mountpoint, ignores nil
// partitions or partitions with an empty mountpoint
func (ep ElementalPartitions) PartitionsByMountPoint(descending bool, excludes ...*Partition) PartitionList {
	mountPointKeys := map[string]*Partition{}
	mountPoints := []string{}
	partitions := PartitionList{}

	for _, p := range ep.PartitionsByInstallOrder([]*Partition{}, excludes...) {
		if p.MountPoint != "" {
			mountPointKeys[p.MountPoint] = p
			mountPoints = append(mountPoints, p.MountPoint)
		}
	}

	if descending {
		sort.Sort(sort.Reverse(sort.StringSlice(mountPoints)))
	} else {
		sort.Strings(mountPoints)
	}

	for _, mnt := range mountPoints {
		partitions = append(partitions, mountPointKeys[mnt])
	}
	return partitions
}

// Image struct represents a file system image with its commonly configurable values, size in MiB
type Image struct {
	File       string
	Label      string       `yaml:"label,omitempty" mapstructure:"label"`
	Size       uint         `yaml:"size,omitempty" mapstructure:"size"`
	FS         string       `yaml:"fs,omitempty" mapstructure:"fs"`
	Source     *ImageSource `yaml:"uri,omitempty" mapstructure:"uri"`
	MountPoint string
	LoopDevice string
}

// InstallState tracks the installation data of the whole system
type InstallState struct {
	Date       string                     `yaml:"date,omitempty"`
	Partitions map[string]*PartitionState `yaml:",omitempty,inline"`
}

// PartitionState tracks installation data of a partition
type PartitionState struct {
	FSLabel string                 `yaml:"label,omitempty"`
	Images  map[string]*ImageState `yaml:",omitempty,inline"`
}

// ImageState represents data of a deployed image
type ImageState struct {
	Source         *ImageSource `yaml:"source,omitempty"`
	SourceMetadata interface{}  `yaml:"source-metadata,omitempty"`
	Label          string       `yaml:"label,omitempty"`
	FS             string       `yaml:"fs,omitempty"`
}

func (i *ImageState) UnmarshalYAML(value *yaml.Node) error {
	type iState ImageState
	var srcMeta *yaml.Node
	var err error

	err = value.Decode((*iState)(i))
	if err != nil {
		return err
	}

	if i.SourceMetadata != nil {
		for i, n := range value.Content {
			if n.Value == "source-metadata" && n.Kind == yaml.ScalarNode {
				if len(value.Content) >= i+1 && value.Content[i+1].Kind == yaml.MappingNode {
					srcMeta = value.Content[i+1]
				}
				break
			}
		}
	}

	i.SourceMetadata = nil
	if srcMeta != nil {
		d := &DockerImageMeta{}
		err = srcMeta.Decode(d)
		if err == nil && (d.Digest != "" || d.Size != 0) {
			i.SourceMetadata = d
			return nil
		}
	}

	return err
}

// DockerImageMeta represents metadata of a docker container image type
type DockerImageMeta struct {
	Digest string `yaml:"digest,omitempty"`
	Size   int64  `yaml:"size,omitempty"`
}

type InstallUkiSpec struct {
	Target          string              `yaml:"device,omitempty" mapstructure:"device"`
	Reboot          bool                `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff        bool                `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
	Partitions      ElementalPartitions `yaml:"partitions,omitempty" mapstructure:"partitions"`
	ExtraPartitions PartitionList       `yaml:"extra-partitions,omitempty" mapstructure:"extra-partitions"`
	CloudInit       []string            `yaml:"cloud-init,omitempty" mapstructure:"cloud-init"`
}

func (i *InstallUkiSpec) Sanitize() error {
	var err error
	return err
}

func (i *InstallUkiSpec) ShouldReboot() bool                 { return i.Reboot }
func (i *InstallUkiSpec) ShouldShutdown() bool               { return i.PowerOff }
func (i *InstallUkiSpec) GetTarget() string                  { return i.Target }
func (i *InstallUkiSpec) GetPartTable() string               { return "gpt" }
func (i *InstallUkiSpec) GetPartitions() ElementalPartitions { return i.Partitions }
func (i *InstallUkiSpec) GetExtraPartitions() PartitionList  { return i.ExtraPartitions }

type UpgradeUkiSpec struct {
	Reboot   bool `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff bool `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
}

func (i *UpgradeUkiSpec) Sanitize() error {
	var err error
	return err
}

func (i *UpgradeUkiSpec) ShouldReboot() bool   { return i.Reboot }
func (i *UpgradeUkiSpec) ShouldShutdown() bool { return i.PowerOff }

type ResetUkiSpec struct {
	Reboot   bool `yaml:"reboot,omitempty" mapstructure:"reboot"`
	PowerOff bool `yaml:"poweroff,omitempty" mapstructure:"poweroff"`
}

func (i *ResetUkiSpec) Sanitize() error {
	var err error
	return err
}

func (i *ResetUkiSpec) ShouldReboot() bool   { return i.Reboot }
func (i *ResetUkiSpec) ShouldShutdown() bool { return i.PowerOff }
