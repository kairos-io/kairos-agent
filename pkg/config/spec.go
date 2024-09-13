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

package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/kairos-io/kairos-sdk/collector"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/jaypipes/ghw"
	"golang.org/x/sys/unix"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"github.com/mitchellh/mapstructure"
	"github.com/sanity-io/litter"
	"github.com/spf13/viper"
)

const (
	_ = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
)

// NewInstallSpec returns an InstallSpec struct all based on defaults and basic host checks (e.g. EFI vs BIOS)
func NewInstallSpec(cfg *Config) (*v1.InstallSpec, error) {
	var firmware string
	var recoveryImg, activeImg, passiveImg v1.Image

	recoveryImgFile := filepath.Join(constants.LiveDir, constants.RecoverySquashFile)

	// Check if current host has EFI firmware
	efiExists, _ := fsutils.Exists(cfg.Fs, constants.EfiDevice)
	// Check the default ISO installation media is available
	isoRootExists, _ := fsutils.Exists(cfg.Fs, constants.IsoBaseTree)
	// Check the default ISO recovery installation media is available)
	recoveryExists, _ := fsutils.Exists(cfg.Fs, recoveryImgFile)

	if efiExists {
		firmware = v1.EFI
	} else {
		firmware = v1.BIOS
	}

	activeImg.Label = constants.ActiveLabel
	activeImg.Size = constants.ImgSize
	activeImg.File = filepath.Join(constants.StateDir, "cOS", constants.ActiveImgFile)
	activeImg.FS = constants.LinuxImgFs
	activeImg.MountPoint = constants.ActiveDir

	// First try to use the install media source
	if isoRootExists {
		activeImg.Source = v1.NewDirSrc(constants.IsoBaseTree)
	}
	// Then any user provided source
	if cfg.Install.Source != "" {
		activeImg.Source, _ = v1.NewSrcFromURI(cfg.Install.Source)
	}
	// If we dont have any just an empty source so the sanitation fails
	// TODO: Should we directly fail here if we got no source instead of waiting for the Sanitize() to fail?
	if !isoRootExists && cfg.Install.Source == "" {
		activeImg.Source = v1.NewEmptySrc()
	}

	if recoveryExists {
		recoveryImg.Source = v1.NewFileSrc(recoveryImgFile)
		recoveryImg.FS = constants.SquashFs
		recoveryImg.File = filepath.Join(constants.RecoveryDir, "cOS", constants.RecoverySquashFile)
		recoveryImg.Size = constants.ImgSize
	} else {
		recoveryImg.Source = v1.NewFileSrc(activeImg.File)
		recoveryImg.FS = constants.LinuxImgFs
		recoveryImg.Label = constants.SystemLabel
		recoveryImg.File = filepath.Join(constants.RecoveryDir, "cOS", constants.RecoveryImgFile)
		recoveryImg.Size = constants.ImgSize
	}

	passiveImg = v1.Image{
		File:   filepath.Join(constants.StateDir, "cOS", constants.PassiveImgFile),
		Label:  constants.PassiveLabel,
		Source: v1.NewFileSrc(activeImg.File),
		FS:     constants.LinuxImgFs,
		Size:   constants.ImgSize,
	}

	spec := &v1.InstallSpec{
		Target:    cfg.Install.Device,
		Firmware:  firmware,
		PartTable: v1.GPT,
		GrubConf:  constants.GrubConf,
		Tty:       constants.DefaultTty,
		Active:    activeImg,
		Recovery:  recoveryImg,
		Passive:   passiveImg,
		NoFormat:  cfg.Install.NoFormat,
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
	} else {
		cfg.Logger.Infof("Setting image size to %dMb", size)
		spec.Active.Size = uint(size)
		spec.Passive.Size = uint(size)
		spec.Recovery.Size = uint(size)
	}

	err = unmarshallFullSpec(cfg, "install", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}

	// Calculate the partitions afterwards so they use the image sizes for the final partition sizes
	spec.Partitions = NewInstallElementalPartitions(cfg.Logger, spec)

	return spec, nil
}

func NewInstallElementalPartitions(log sdkTypes.KairosLogger, spec *v1.InstallSpec) v1.ElementalPartitions {
	pt := v1.ElementalPartitions{}
	var oemSize uint
	if spec.Partitions.OEM != nil && spec.Partitions.OEM.Size != 0 {
		oemSize = spec.Partitions.OEM.Size
	} else {
		oemSize = constants.OEMSize
	}
	pt.OEM = &v1.Partition{
		FilesystemLabel: constants.OEMLabel,
		Size:            oemSize,
		Name:            constants.OEMPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.OEMDir,
		Flags:           []string{},
	}
	log.Infof("Setting OEM partition size to %dMb", oemSize)
	// Double the space for recovery, as upgrades use the recovery partition to create the transition image for upgrades
	// so we need twice the space to do a proper upgrade
	// Check if the default/user provided values are enough to fit the images sizes
	var recoverySize uint
	if spec.Partitions.Recovery == nil { // This means its not configured by user so use the default
		recoverySize = (spec.Recovery.Size * 2) + 200
	} else {
		if spec.Partitions.Recovery.Size < (spec.Recovery.Size*2)+200 { // Configured by user but not enough space
			// If we had the logger here we could log a message saying that space is not enough and we are auto increasing it
			recoverySize = (spec.Recovery.Size * 2) + 200
			log.Warnf("Not enough space set for recovery partition(%dMb), increasing it to fit the recovery images(%dMb)", spec.Partitions.Recovery.Size, recoverySize)
		} else {
			recoverySize = spec.Partitions.Recovery.Size
		}
	}
	log.Infof("Setting recovery partition size to %dMb", recoverySize)
	pt.Recovery = &v1.Partition{
		FilesystemLabel: constants.RecoveryLabel,
		Size:            recoverySize,
		Name:            constants.RecoveryPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.RecoveryDir,
		Flags:           []string{},
	}

	// Add 1 Gb to the partition so images can grow a bit, otherwise you are stuck with the smallest space possible and
	// there is no coming back from that
	// Also multiply the space for active, as upgrades use the state partition to create the transition image for upgrades
	// so we need twice the space to do a proper upgrade
	// Check if the default/user provided values are enough to fit the images sizes
	var stateSize uint
	if spec.Partitions.State == nil { // This means its not configured by user so use the default
		stateSize = (spec.Active.Size * 2) + spec.Passive.Size + 1000
	} else {
		if spec.Partitions.State.Size < (spec.Active.Size*2)+spec.Passive.Size+1000 { // Configured by user but not enough space
			stateSize = (spec.Active.Size * 2) + spec.Passive.Size + 1000
			log.Warnf("Not enough space set for state partition(%dMb), increasing it to fit the state images(%dMb)", spec.Partitions.State.Size, stateSize)
		} else {
			stateSize = spec.Partitions.State.Size
		}
	}
	log.Infof("Setting state partition size to %dMb", stateSize)
	pt.State = &v1.Partition{
		FilesystemLabel: constants.StateLabel,
		Size:            stateSize,
		Name:            constants.StatePartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.StateDir,
		Flags:           []string{},
	}
	var persistentSize uint
	if spec.Partitions.Persistent == nil { // This means its not configured by user so use the default
		persistentSize = constants.PersistentSize
	} else {
		persistentSize = spec.Partitions.Persistent.Size
	}
	pt.Persistent = &v1.Partition{
		FilesystemLabel: constants.PersistentLabel,
		Size:            persistentSize,
		Name:            constants.PersistentPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.PersistentDir,
		Flags:           []string{},
	}
	log.Infof("Setting persistent partition size to %dMb", persistentSize)
	return pt
}

// NewUpgradeSpec returns an UpgradeSpec struct all based on defaults and current host state
func NewUpgradeSpec(cfg *Config) (*v1.UpgradeSpec, error) {
	var recLabel, recFs, recMnt string
	var active, passive, recovery v1.Image

	installState, err := cfg.LoadInstallState()
	if err != nil {
		cfg.Logger.Warnf("failed reading installation state: %s", err.Error())
	}

	parts, err := partitions.GetAllPartitions()
	if err != nil {
		return nil, fmt.Errorf("could not read host partitions")
	}
	ep := v1.NewElementalPartitionsFromList(parts)

	if ep.Recovery == nil {
		// We could have recovery in lvm which won't appear in ghw list
		ep.Recovery = partitions.GetPartitionViaDM(cfg.Fs, constants.RecoveryLabel)
	}

	if ep.OEM == nil {
		// We could have OEM in lvm which won't appear in ghw list
		ep.OEM = partitions.GetPartitionViaDM(cfg.Fs, constants.OEMLabel)
	}

	if ep.Persistent == nil {
		// We could have persistent encrypted or in lvm which won't appear in ghw list
		ep.Persistent = partitions.GetPartitionViaDM(cfg.Fs, constants.PersistentLabel)
	}

	if ep.Recovery != nil {
		if ep.Recovery.MountPoint == "" {
			ep.Recovery.MountPoint = constants.RecoveryDir
		}

		squashedRec, err := hasSquashedRecovery(cfg, ep.Recovery)
		if err != nil {
			return nil, fmt.Errorf("failed checking for squashed recovery: %w", err)
		}

		if squashedRec {
			recFs = constants.SquashFs
		} else {
			recLabel = constants.SystemLabel
			recFs = constants.LinuxImgFs
			recMnt = constants.TransitionDir
		}

		recovery = v1.Image{
			File:       filepath.Join(ep.Recovery.MountPoint, "cOS", constants.TransitionImgFile),
			Size:       constants.ImgSize,
			Label:      recLabel,
			FS:         recFs,
			MountPoint: recMnt,
			Source:     v1.NewEmptySrc(),
		}
	}

	if ep.State != nil {
		if ep.State.MountPoint == "" {
			ep.State.MountPoint = constants.StateDir
		}

		active = v1.Image{
			File:       filepath.Join(ep.State.MountPoint, "cOS", constants.TransitionImgFile),
			Size:       constants.ImgSize,
			Label:      constants.ActiveLabel,
			FS:         constants.LinuxImgFs,
			MountPoint: constants.TransitionDir,
			Source:     v1.NewEmptySrc(),
		}

		passive = v1.Image{
			File:   filepath.Join(ep.State.MountPoint, "cOS", constants.PassiveImgFile),
			Label:  constants.PassiveLabel,
			Size:   constants.ImgSize,
			Source: v1.NewFileSrc(active.File),
			FS:     active.FS,
		}
	}

	// If we have oem in the system, but it has no mountpoint
	if ep.OEM != nil && ep.OEM.MountPoint == "" {
		// Add the default mountpoint for it in case the chroot stages want to bind mount it
		ep.OEM.MountPoint = constants.OEMPath
	}
	// This is needed if we want to use the persistent as tmpdir for the upgrade images
	// as tmpfs is 25% of the total RAM, we cannot rely on the tmp dir having enough space for our image
	// This enables upgrades on low ram devices
	if ep.Persistent != nil {
		if ep.Persistent.MountPoint == "" {
			ep.Persistent.MountPoint = constants.PersistentDir
		}
	}

	// Deep look to see if upgrade.recovery == true in the config
	// if yes, we set the upgrade spec "Entry" to "recovery"
	entry := ""
	_, ok := cfg.Config["upgrade"]
	if ok {
		_, ok = cfg.Config["upgrade"].(collector.Config)["recovery"]
		if ok {
			if cfg.Config["upgrade"].(collector.Config)["recovery"].(bool) {
				entry = constants.BootEntryRecovery
			}
		}
	}

	spec := &v1.UpgradeSpec{
		Entry:      entry,
		Active:     active,
		Recovery:   recovery,
		Passive:    passive,
		Partitions: ep,
		State:      installState,
	}

	// Unmarshall the config into the spec first so the active/recovery gets filled properly from all sources
	err = unmarshallFullSpec(cfg, "upgrade", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}
	err = setUpgradeSourceSize(cfg, spec)
	if err != nil {
		return nil, fmt.Errorf("failed calculating size: %w", err)
	}
	return spec, nil
}

func setUpgradeSourceSize(cfg *Config, spec *v1.UpgradeSpec) error {
	var size int64
	var err error

	var targetSpec *v1.Image
	if spec.RecoveryUpgrade() {
		targetSpec = &(spec.Recovery)
	} else {
		targetSpec = &(spec.Active)
	}

	if targetSpec.Source != nil && targetSpec.Source.IsEmpty() {
		return nil
	}

	size, err = GetSourceSize(cfg, targetSpec.Source)
	if err != nil {
		return err
	}

	cfg.Logger.Infof("Setting image size to %dMb", size)
	targetSpec.Size = uint(size)

	return nil
}

// NewResetSpec returns a ResetSpec struct all based on defaults and current host state
func NewResetSpec(cfg *Config) (*v1.ResetSpec, error) {
	var imgSource *v1.ImageSource

	//TODO find a way to pre-load current state values such as labels
	if !BootedFrom(cfg.Runner, constants.RecoverySquashFile) &&
		!BootedFrom(cfg.Runner, constants.SystemLabel) {
		return nil, fmt.Errorf("reset can only be called from the recovery system")
	}

	efiExists, _ := fsutils.Exists(cfg.Fs, constants.EfiDevice)

	installState, err := cfg.LoadInstallState()
	if err != nil {
		cfg.Logger.Warnf("failed reading installation state: %s", err.Error())
	}

	parts, err := partitions.GetAllPartitions()
	if err != nil {
		return nil, fmt.Errorf("could not read host partitions")
	}
	ep := v1.NewElementalPartitionsFromList(parts)

	if efiExists {
		if ep.EFI == nil {
			return nil, fmt.Errorf("EFI partition not found")
		}
		if ep.EFI.MountPoint == "" {
			ep.EFI.MountPoint = constants.EfiDir
		}
		ep.EFI.Name = constants.EfiPartName
	}

	if ep.State == nil {
		return nil, fmt.Errorf("state partition not found")
	}
	if ep.State.MountPoint == "" {
		ep.State.MountPoint = constants.StateDir
	}
	ep.State.Name = constants.StatePartName

	if ep.Recovery == nil {
		// We could have recovery in lvm which won't appear in ghw list
		ep.Recovery = partitions.GetPartitionViaDM(cfg.Fs, constants.RecoveryLabel)
		if ep.Recovery == nil {
			return nil, fmt.Errorf("recovery partition not found")
		}
	}
	if ep.Recovery.MountPoint == "" {
		ep.Recovery.MountPoint = constants.RecoveryDir
	}

	target := ep.State.Disk

	// OEM partition is not a hard requirement for reset unless we have the reset oem flag
	cfg.Logger.Info(litter.Sdump(ep.OEM))
	if ep.OEM == nil {
		// We could have oem in lvm which won't appear in ghw list
		ep.OEM = partitions.GetPartitionViaDM(cfg.Fs, constants.OEMLabel)
	}

	// Persistent partition is not a hard requirement
	if ep.Persistent == nil {
		// We could have persistent encrypted or in lvm which won't appear in ghw list
		ep.Persistent = partitions.GetPartitionViaDM(cfg.Fs, constants.PersistentLabel)
	}

	recoveryImg := filepath.Join(constants.RunningStateDir, "cOS", constants.RecoveryImgFile)
	recoveryImg2 := filepath.Join(constants.RunningRecoveryStateDir, "cOS", constants.RecoveryImgFile)

	if exists, _ := fsutils.Exists(cfg.Fs, recoveryImg); exists {
		imgSource = v1.NewFileSrc(recoveryImg)
	} else if exists, _ = fsutils.Exists(cfg.Fs, recoveryImg2); exists {
		imgSource = v1.NewFileSrc(recoveryImg2)
	} else if exists, _ = fsutils.Exists(cfg.Fs, constants.IsoBaseTree); exists {
		imgSource = v1.NewDirSrc(constants.IsoBaseTree)
	} else {
		imgSource = v1.NewEmptySrc()
	}

	activeFile := filepath.Join(ep.State.MountPoint, "cOS", constants.ActiveImgFile)
	spec := &v1.ResetSpec{
		Target:           target,
		Partitions:       ep,
		Efi:              efiExists,
		GrubDefEntry:     constants.GrubDefEntry,
		GrubConf:         constants.GrubConf,
		Tty:              constants.DefaultTty,
		FormatPersistent: true,
		Active: v1.Image{
			Label:      constants.ActiveLabel,
			Size:       constants.ImgSize,
			File:       activeFile,
			FS:         constants.LinuxImgFs,
			Source:     imgSource,
			MountPoint: constants.ActiveDir,
		},
		Passive: v1.Image{
			File:   filepath.Join(ep.State.MountPoint, "cOS", constants.PassiveImgFile),
			Label:  constants.PassiveLabel,
			Size:   constants.ImgSize,
			Source: v1.NewFileSrc(activeFile),
			FS:     constants.LinuxImgFs,
		},
		State: installState,
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
	} else {
		cfg.Logger.Infof("Setting image size to %dMb", size)
		spec.Active.Size = uint(size)
		spec.Passive.Size = uint(size)
	}

	err = unmarshallFullSpec(cfg, "reset", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}

	if ep.OEM == nil && spec.FormatOEM {
		cfg.Logger.Warnf("no OEM partition found, won't format it")
	}

	if ep.Persistent == nil && spec.FormatPersistent {
		cfg.Logger.Warnf("no Persistent partition found, won't format it")
	}

	// If we mount partitions by the /dev/disk/by-label stanza, their mountpoints wont show up due to ghw not
	// accounting for them. Normally this is not an issue but in this case, if we are formatting a given partition we
	// want to unmount it first. Thus we need to make sure that if its mounted we have the mountpoint info
	if ep.Persistent.MountPoint == "" {
		ep.Persistent.MountPoint = partitions.GetMountPointByLabel(ep.Persistent.FilesystemLabel)
	}

	if ep.OEM.MountPoint == "" {
		ep.OEM.MountPoint = partitions.GetMountPointByLabel(ep.OEM.FilesystemLabel)
	}

	return spec, nil
}

// ReadResetSpecFromConfig will return a proper v1.ResetSpec based on an agent Config
func ReadResetSpecFromConfig(c *Config) (*v1.ResetSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "reset")
	if err != nil {
		return &v1.ResetSpec{}, err
	}
	resetSpec := sp.(*v1.ResetSpec)
	return resetSpec, nil
}

func NewUkiResetSpec(cfg *Config) (spec *v1.ResetUkiSpec, err error) {
	spec = &v1.ResetUkiSpec{
		FormatPersistent: true, // Persistent is formatted by default
		Partitions:       v1.ElementalPartitions{},
	}

	_, ukiBootMode := cfg.Fs.Stat("/run/cos/uki_boot_mode")
	if !BootedFrom(cfg.Runner, "rd.immucore.uki") && ukiBootMode == nil {
		return spec, fmt.Errorf("uki reset can only be called from the recovery installed system")
	}

	// Fill persistent partition
	spec.Partitions.Persistent = partitions.GetPartitionViaDM(cfg.Fs, constants.PersistentLabel)
	spec.Partitions.OEM = partitions.GetPartitionViaDM(cfg.Fs, constants.OEMLabel)

	// Get EFI partition
	parts, err := partitions.GetAllPartitions()
	if err != nil {
		return spec, fmt.Errorf("could not read host partitions")
	}
	for _, p := range parts {
		if p.FilesystemLabel == constants.EfiLabel {
			spec.Partitions.EFI = p
			break
		}
	}

	if spec.Partitions.Persistent == nil {
		return spec, fmt.Errorf("persistent partition not found")
	}
	if spec.Partitions.OEM == nil {
		return spec, fmt.Errorf("oem partition not found")
	}
	if spec.Partitions.EFI == nil {
		return spec, fmt.Errorf("efi partition not found")
	}

	// Fill oem partition
	err = unmarshallFullSpec(cfg, "reset", spec)
	return spec, err
}

// ReadInstallSpecFromConfig will return a proper v1.InstallSpec based on an agent Config
func ReadInstallSpecFromConfig(c *Config) (*v1.InstallSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "install")
	if err != nil {
		return &v1.InstallSpec{}, err
	}
	installSpec := sp.(*v1.InstallSpec)

	if (installSpec.Target == "" || installSpec.Target == "auto") && !installSpec.NoFormat {
		installSpec.Target = detectLargestDevice()
	}

	return installSpec, nil
}

// ReadUkiResetSpecFromConfig will return a proper v1.ResetUkiSpec based on an agent Config
func ReadUkiResetSpecFromConfig(c *Config) (*v1.ResetUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "reset-uki")
	if err != nil {
		return &v1.ResetUkiSpec{}, err
	}
	resetSpec := sp.(*v1.ResetUkiSpec)
	return resetSpec, nil
}

func NewUkiInstallSpec(cfg *Config) (*v1.InstallUkiSpec, error) {
	var activeImg v1.Image

	isoRootExists, _ := fsutils.Exists(cfg.Fs, constants.IsoBaseTree)

	// First try to use the install media source
	if isoRootExists {
		activeImg.Source = v1.NewDirSrc(constants.IsoBaseTree)
	}
	// Then any user provided source
	if cfg.Install.Source != "" {
		activeImg.Source, _ = v1.NewSrcFromURI(cfg.Install.Source)
	}
	// If we dont have any just an empty source so the sanitation fails
	// TODO: Should we directly fail here if we got no source instead of waiting for the Sanitize() to fail?
	if !isoRootExists && cfg.Install.Source == "" {
		activeImg.Source = v1.NewEmptySrc()
	}

	spec := &v1.InstallUkiSpec{
		Target: cfg.Install.Device,
		Active: activeImg,
	}

	// Calculate the partitions afterwards so they use the image sizes for the final partition sizes
	spec.Partitions.EFI = &v1.Partition{
		FilesystemLabel: constants.EfiLabel,
		Size:            constants.ImgSize * 5, // 15Gb for the EFI partition as default
		Name:            constants.EfiPartName,
		FS:              constants.EfiFs,
		MountPoint:      constants.EfiDir,
		Flags:           []string{"esp"},
	}
	spec.Partitions.OEM = &v1.Partition{
		FilesystemLabel: constants.OEMLabel,
		Size:            constants.OEMSize,
		Name:            constants.OEMPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.OEMDir,
		Flags:           []string{},
	}
	spec.Partitions.Persistent = &v1.Partition{
		FilesystemLabel: constants.PersistentLabel,
		Size:            constants.PersistentSize,
		Name:            constants.PersistentPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.PersistentDir,
		Flags:           []string{},
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images, leaving it as default size (%dMb): %s", spec.Partitions.EFI.Size, err.Error())
	} else {
		// Only override if the calculated size is bigger than the default size, otherwise stay with 15Gb minimum
		if uint(size*3) > spec.Partitions.EFI.Size {
			spec.Partitions.EFI.Size = uint(size * 3)
		}
	}

	cfg.Logger.Infof("Setting image size to %dMb", spec.Partitions.EFI.Size)

	err = unmarshallFullSpec(cfg, "install", spec)

	// Add default values for the skip partitions for our default entries
	spec.SkipEntries = append(spec.SkipEntries, constants.UkiDefaultSkipEntries()...)
	return spec, err
}

// ReadUkiInstallSpecFromConfig will return a proper v1.InstallUkiSpec based on an agent Config
func ReadUkiInstallSpecFromConfig(c *Config) (*v1.InstallUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "install-uki")
	if err != nil {
		return &v1.InstallUkiSpec{}, err
	}
	installSpec := sp.(*v1.InstallUkiSpec)

	if (installSpec.Target == "" || installSpec.Target == "auto") && !installSpec.NoFormat {
		installSpec.Target = detectLargestDevice()
	}

	return installSpec, nil
}

func NewUkiUpgradeSpec(cfg *Config) (*v1.UpgradeUkiSpec, error) {
	spec := &v1.UpgradeUkiSpec{}
	if err := unmarshallFullSpec(cfg, "upgrade", spec); err != nil {
		return nil, fmt.Errorf("failed unmarshalling full spec: %w", err)
	}
	// TODO: Use this everywhere?
	cfg.Logger.Infof("Checking if OCI image %s exists", spec.Active.Source.Value())
	if spec.Active.Source.IsDocker() {
		_, err := crane.Manifest(spec.Active.Source.Value())
		if err != nil {
			if strings.Contains(err.Error(), "MANIFEST_UNKNOWN") {
				return nil, fmt.Errorf("oci image %s does not exist", spec.Active.Source.Value())
			}
			return nil, err
		}
	}

	// Get the actual source size to calculate the image size and partitions size
	size, err := GetSourceSize(cfg, spec.Active.Source)
	if err != nil {
		cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
		spec.Active.Size = constants.ImgSize
	} else {
		cfg.Logger.Infof("Setting image size to %dMb", size)
		spec.Active.Size = uint(size)
	}

	// Get EFI partition
	parts, err := partitions.GetAllPartitions()
	if err != nil {
		return spec, fmt.Errorf("could not read host partitions")
	}
	for _, p := range parts {
		if p.FilesystemLabel == constants.EfiLabel {
			spec.EfiPartition = p
			break
		}
	}
	// Get free size of partition
	var stat unix.Statfs_t
	_ = unix.Statfs(spec.EfiPartition.MountPoint, &stat)
	freeSize := stat.Bfree * uint64(stat.Bsize) / 1000 / 1000
	cfg.Logger.Debugf("Partition on mountpoint %s has %dMb free", spec.EfiPartition.MountPoint, freeSize)
	// Check if the source is over the free size
	if spec.Active.Size > uint(freeSize) {
		return spec, fmt.Errorf("source size(%d) is bigger than the free space(%d) on the EFI partition(%s)", spec.Active.Size, freeSize, spec.EfiPartition.MountPoint)
	}

	return spec, err
}

// ReadUkiUpgradeSpecFromConfig will return a proper v1.UpgradeUkiSpec based on an agent Config
func ReadUkiUpgradeSpecFromConfig(c *Config) (*v1.UpgradeUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "upgrade-uki")
	if err != nil {
		return &v1.UpgradeUkiSpec{}, err
	}
	upgradeSpec := sp.(*v1.UpgradeUkiSpec)
	return upgradeSpec, nil
}

// getSize will calculate the size of a file or symlink and will do nothing with directories
// fileList: keeps track of the files visited to avoid counting a file more than once if it's a symlink. It could also be used as a way to filter some files
// size: will be the memory that adds up all the files sizes. Meaning it could be initialized with a value greater than 0 if needed.
func getSize(size *int64, fileList map[string]bool, path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}

	if d.IsDir() {
		return nil
	}

	actualFilePath := path
	if d.Type()&fs.ModeSymlink != 0 {
		// If it's a symlink, get its target and calculate its size.
		var err error
		actualFilePath, err = os.Readlink(path)
		if err != nil {
			return err
		}

		if !filepath.IsAbs(actualFilePath) {
			// If it's a relative path, join it with the base directory path.
			actualFilePath = filepath.Join(filepath.Dir(path), actualFilePath)
		}
	}

	fileInfo, err := os.Stat(actualFilePath)
	if os.IsNotExist(err) || fileList[actualFilePath] {
		return nil
	}
	if err != nil {
		return err
	}

	*size += fileInfo.Size()
	fileList[actualFilePath] = true

	return nil
}

// GetSourceSize will try to gather the actual size of the source
// Useful to create the exact size of images and by side effect the partition size
// This helps adjust the size to be juuuuust right.
// It can still be manually override from the cloud config by setting all values manually
// But by default it should adjust the sizes properly
func GetSourceSize(config *Config, source *v1.ImageSource) (int64, error) {
	var size int64
	var err error
	var filesVisited map[string]bool

	switch {
	case source.IsDocker():
		// Docker size is uncompressed! So we double it to be sure that we have enough space
		// Otherwise we would need to READ all the layers uncompressed to calculate the image size which would
		// double the download size and slow down everything
		size, err = config.ImageExtractor.GetOCIImageSize(source.Value(), config.Platform.String())
		size = int64(float64(size) * 2.5)
	case source.IsDir():
		filesVisited = make(map[string]bool, 30000) // An Ubuntu system has around 27k files. This improves performance by not having to resize the map for every file visited
		hostDir := os.Getenv("HOST_DIR")
		err = fsutils.WalkDirFs(config.Fs, source.Value(), func(path string, d fs.DirEntry, err error) error {
			// In kubernetes we use the suc script to upgrade, which mounts the host root into $HOST_DIR
			// we should skip that dir when calculating the size as we would be doubling the calculated size
			// Plus we will hit the usual things when checking a running system. Processes that go away, tmpfiles, etc...
			// If its empty we are just not setting it, so probably out of the k8s upgrade path
			if hostDir != "" && strings.HasPrefix(path, hostDir) {
				config.Logger.Logger.Debug().Str("path", path).Str("hostDir", hostDir).Msg("Skipping file as it is a host directory")
			} else {
				v := getSize(&size, filesVisited, path, d, err)
				return v
			}

			return nil
		})

	case source.IsFile():
		file, err := config.Fs.Stat(source.Value())
		if err != nil {
			return size, err
		}
		size = file.Size()
	}
	// Normalize size to Mb before returning and add 100Mb to round the size from bytes to mb+extra files like grub stuff
	if size != 0 {
		size = (size / 1000 / 1000) + 100
	}
	return size, err
}

// ReadUpgradeSpecFromConfig will return a proper v1.UpgradeSpec based on an agent Config
func ReadUpgradeSpecFromConfig(c *Config) (*v1.UpgradeSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "upgrade")
	if err != nil {
		return &v1.UpgradeSpec{}, err
	}
	upgradeSpec := sp.(*v1.UpgradeSpec)
	return upgradeSpec, nil
}

// ReadSpecFromCloudConfig returns a v1.Spec for the given spec
func ReadSpecFromCloudConfig(r *Config, spec string) (v1.Spec, error) {
	var sp v1.Spec
	var err error

	switch spec {
	case "install":
		sp, err = NewInstallSpec(r)
	case "upgrade":
		sp, err = NewUpgradeSpec(r)
	case "reset":
		sp, err = NewResetSpec(r)
	case "install-uki":
		sp, err = NewUkiInstallSpec(r)
	case "reset-uki":
		sp, err = NewUkiResetSpec(r)
	case "upgrade-uki":
		sp, err = NewUkiUpgradeSpec(r)
	default:
		return nil, fmt.Errorf("spec not valid: %s", spec)
	}
	if err != nil {
		return nil, fmt.Errorf("failed initializing spec: %v", err)
	}

	err = sp.Sanitize()
	if err != nil {
		return sp, fmt.Errorf("sanitizing the %s spec: %w", spec, err)
	}

	r.Logger.Debugf("Loaded %s spec: %s", spec, litter.Sdump(sp))
	return sp, nil
}

var decodeHook = viper.DecodeHook(
	mapstructure.ComposeDecodeHookFunc(
		UnmarshalerHook(),
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	),
)

type Unmarshaler interface {
	CustomUnmarshal(interface{}) (bool, error)
}

func UnmarshalerHook() mapstructure.DecodeHookFunc {
	return func(from reflect.Value, to reflect.Value) (interface{}, error) {
		// get the destination object address if it is not passed by reference
		if to.CanAddr() {
			to = to.Addr()
		}
		// If the destination implements the unmarshaling interface
		u, ok := to.Interface().(Unmarshaler)
		if !ok {
			return from.Interface(), nil
		}
		// If it is nil and a pointer, create and assign the target value first
		if to.IsNil() && to.Type().Kind() == reflect.Ptr {
			to.Set(reflect.New(to.Type().Elem()))
			u = to.Interface().(Unmarshaler)
		}
		// Call the custom unmarshaling method
		cont, err := u.CustomUnmarshal(from.Interface())
		if cont {
			// Continue with the decoding stack
			return from.Interface(), err
		}
		// Decoding finalized
		return to.Interface(), err
	}
}

func setDecoder(config *mapstructure.DecoderConfig) {
	// Make sure we zero fields before applying them, this is relevant for slices
	// so we do not merge with any already present value and directly apply whatever
	// we got form configs.
	config.ZeroFields = true
}

// BootedFrom will check if we are booting from the given label
func BootedFrom(runner v1.Runner, label string) bool {
	out, _ := runner.Run("cat", "/proc/cmdline")
	return strings.Contains(string(out), label)
}

// HasSquashedRecovery returns true if a squashed recovery image is found in the system
func hasSquashedRecovery(config *Config, recovery *v1.Partition) (squashed bool, err error) {
	mountPoint := recovery.MountPoint
	if mnt, _ := isMounted(config, recovery); !mnt {
		tmpMountDir, err := fsutils.TempDir(config.Fs, "", "elemental")
		if err != nil {
			config.Logger.Errorf("failed creating temporary dir: %v", err)
			return false, err
		}
		defer config.Fs.RemoveAll(tmpMountDir) // nolint:errcheck
		err = config.Mounter.Mount(recovery.Path, tmpMountDir, "auto", []string{})
		if err != nil {
			config.Logger.Errorf("failed mounting recovery partition: %v", err)
			return false, err
		}
		mountPoint = tmpMountDir
		defer func() {
			err = config.Mounter.Unmount(tmpMountDir)
			if err != nil {
				squashed = false
			}
		}()
	}
	return fsutils.Exists(config.Fs, filepath.Join(mountPoint, "cOS", constants.RecoverySquashFile))
}

func isMounted(config *Config, part *v1.Partition) (bool, error) {
	if part == nil {
		return false, fmt.Errorf("nil partition")
	}

	if part.MountPoint == "" {
		return false, nil
	}
	// Using IsLikelyNotMountPoint seams to be safe as we are not checking
	// for bind mounts here
	notMnt, err := config.Mounter.IsLikelyNotMountPoint(part.MountPoint)
	if err != nil {
		return false, err
	}
	return !notMnt, nil
}

func unmarshallFullSpec(r *Config, subkey string, sp v1.Spec) error {
	// Load the config into viper from the raw cloud config string
	ccString, err := r.String()
	if err != nil {
		return fmt.Errorf("failed initializing spec: %w", err)
	}
	viper.SetConfigType("yaml")
	viper.ReadConfig(strings.NewReader(ccString))
	vp := viper.Sub(subkey)
	if vp == nil {
		vp = viper.New()
	}

	err = vp.Unmarshal(sp, setDecoder, decodeHook)
	if err != nil {
		return fmt.Errorf("error unmarshalling %s Spec: %w", subkey, err)
	}

	return nil
}

// detectLargestDevice returns the largest disk found
func detectLargestDevice() string {
	preferedDevice := "/dev/sda"
	maxSize := float64(0)

	block, err := ghw.Block()
	if err == nil {
		for _, disk := range block.Disks {
			size := float64(disk.SizeBytes) / float64(GiB)
			if size > maxSize {
				maxSize = size
				preferedDevice = "/dev/" + disk.Name
			}
		}
	}
	return preferedDevice
}

// DetectPreConfiguredDevice returns a disk that has partitions labeled with
// Kairos labels. It can be used to detect a pre-configured device.
func DetectPreConfiguredDevice(logger sdkTypes.KairosLogger) (string, error) {
	block, err := ghw.Block()
	if err != nil {
		logger.Errorf("failed getting block devices: %s", err.Error())
		return "", err
	}

	for _, disk := range block.Disks {
		for _, p := range disk.Partitions {
			if p.FilesystemLabel == "COS_STATE" {
				return filepath.Join("/", "dev", disk.Name), nil
			}
		}
	}

	return "", nil
}
