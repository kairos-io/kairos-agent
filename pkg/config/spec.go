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

	"github.com/kairos-io/kairos-agent/v2/internal/common"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"github.com/mitchellh/mapstructure"
	"github.com/sanity-io/litter"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
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

	if isoRootExists {
		activeImg.Source = v1.NewDirSrc(constants.IsoBaseTree)
	} else {
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
	spec.Partitions = NewInstallElementalPartitions(spec)

	return spec, nil
}

func NewInstallElementalPartitions(spec *v1.InstallSpec) v1.ElementalPartitions {
	pt := v1.ElementalPartitions{}
	pt.OEM = &v1.Partition{
		FilesystemLabel: constants.OEMLabel,
		Size:            constants.OEMSize,
		Name:            constants.OEMPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.OEMDir,
		Flags:           []string{},
	}

	// Double the space for recovery, as upgrades use the recovery partition to create the transition image for upgrades
	// so we need twice the space to do a proper upgrade
	pt.Recovery = &v1.Partition{
		FilesystemLabel: constants.RecoveryLabel,
		Size:            (spec.Recovery.Size * 2) + 200,
		Name:            constants.RecoveryPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.RecoveryDir,
		Flags:           []string{},
	}

	// Add 1 Gb to the partition so images can grow a bit, otherwise you are stuck with the smallest space possible and
	// there is no coming back from that
	// Also multiply the space for active, as upgrades use the state partition to create the transition image for upgrades
	// so we need twice the space to do a proper upgrade
	pt.State = &v1.Partition{
		FilesystemLabel: constants.StateLabel,
		Size:            (spec.Active.Size * 2) + spec.Passive.Size + 1000,
		Name:            constants.StatePartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.StateDir,
		Flags:           []string{},
	}

	pt.Persistent = &v1.Partition{
		FilesystemLabel: constants.PersistentLabel,
		Size:            constants.PersistentSize,
		Name:            constants.PersistentPartName,
		FS:              constants.LinuxFs,
		MountPoint:      constants.PersistentDir,
		Flags:           []string{},
	}
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

	spec := &v1.UpgradeSpec{
		Active:     active,
		Recovery:   recovery,
		Passive:    passive,
		Partitions: ep,
		State:      installState,
	}

	setUpgradeSourceSize(cfg, spec)

	err = unmarshallFullSpec(cfg, "upgrade", spec)
	if err != nil {
		return nil, fmt.Errorf("failed unmarshalling the full spec: %w", err)
	}

	return spec, nil
}

func setUpgradeSourceSize(cfg *Config, spec *v1.UpgradeSpec) error {
	var size int64
	var err error

	var targetSpec *v1.Image
	if spec.RecoveryUpgrade {
		targetSpec = &(spec.Recovery)
	} else {
		targetSpec = &(spec.Active)
	}

	if targetSpec.Source == nil || targetSpec.Source.IsEmpty() {
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

	// OEM partition is not a hard requirement
	if ep.OEM != nil {
		if ep.OEM.MountPoint == "" {
			ep.OEM.MountPoint = constants.OEMDir
		}
		ep.OEM.Name = constants.OEMPartName
	} else {
		// We could have oem in lvm which won't appear in ghw list
		ep.OEM = partitions.GetPartitionViaDM(cfg.Fs, constants.OEMLabel)
	}

	if ep.OEM == nil {
		cfg.Logger.Warnf("no OEM partition found")
	}

	// Persistent partition is not a hard requirement
	if ep.Persistent != nil {
		if ep.Persistent.MountPoint == "" {
			ep.Persistent.MountPoint = constants.PersistentDir
		}
		ep.Persistent.Name = constants.PersistentPartName
	} else {
		// We could have persistent encrypted or in lvm which won't appear in ghw list
		ep.Persistent = partitions.GetPartitionViaDM(cfg.Fs, constants.PersistentLabel)
	}
	if ep.Persistent == nil {
		cfg.Logger.Warnf("no Persistent partition found")
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

// ReadInstallSpecFromConfig will return a proper v1.InstallSpec based on an agent Config
func ReadInstallSpecFromConfig(c *Config) (*v1.InstallSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "install")
	if err != nil {
		return &v1.InstallSpec{}, err
	}
	installSpec := sp.(*v1.InstallSpec)
	// Workaround!
	// If we set the "auto" for the device in the cloudconfig the value will be proper in the Config.Install.Device
	// But on the cloud-config it will still appear as "auto" as we dont modify that
	// Unfortunately as we load the full cloud-config and unmarshall it into our spec, we cannot infer from there
	// What device was choosen, and re-choosing again could lead to different results
	// So instead we do the check here and override the installSpec.Target with the Config.Install.Device
	// as its the soonest we have access to both
	if installSpec.Target == "auto" {
		installSpec.Target = c.Install.Device
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
	spec := &v1.InstallUkiSpec{
		Target: cfg.Install.Device,
	}

	// Calculate the partitions afterwards so they use the image sizes for the final partition sizes
	spec.Partitions.EFI = &v1.Partition{
		FilesystemLabel: constants.EfiLabel,
		Size:            constants.ImgSize, // TODO: Fix this and set proper size based on the source size
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

	// TODO: Which key to use? install or install-uki?
	err := unmarshallFullSpec(cfg, "install", spec)

	return spec, err
}

// ReadUkiInstallSpecFromConfig will return a proper v1.InstallUkiSpec based on an agent Config
func ReadUkiInstallSpecFromConfig(c *Config) (*v1.InstallUkiSpec, error) {
	sp, err := ReadSpecFromCloudConfig(c, "install-uki")
	if err != nil {
		return &v1.InstallUkiSpec{}, err
	}
	installSpec := sp.(*v1.InstallUkiSpec)
	return installSpec, nil
}

// ReadUkiUpgradeFromConfig will return a proper v1.UpgradeUkiSpec based on an agent Config
func ReadUkiUpgradeFromConfig(c *Config) (*v1.UpgradeUkiSpec, error) {
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

		err = fsutils.WalkDirFs(config.Fs, source.Value(), func(path string, d fs.DirEntry, err error) error {
			v := getSize(&size, filesVisited, path, d, err)

			return v
		})

	case source.IsFile():
		file, err := config.Fs.Stat(source.Value())
		if err == nil {
			size = file.Size()
		}
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
		// TODO: Fill with proper defaults
		sp = &v1.ResetUkiSpec{}
	case "upgrade-uki":
		// TODO: Fill with proper defaults
		sp = &v1.UpgradeUkiSpec{}
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

func configLogger(log v1.Logger, vfs v1.FS) {
	// Set debug level
	if viper.GetBool("debug") {
		log.SetLevel(v1.DebugLevel())
	}

	// Set formatter so both file and stdout format are equal
	log.SetFormatter(&logrus.TextFormatter{
		ForceColors:      true,
		DisableColors:    false,
		DisableTimestamp: false,
		FullTimestamp:    true,
	})

	// Logfile
	// Not being used for now, disable it until we plug it again in our cli
	/*
		logfile := viper.GetString("logfile")
		if logfile != "" {
			o, err := vfs.OpenFile(logfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fs.ModePerm)

			if err != nil {
				log.Errorf("Could not open %s for logging to file: %s", logfile, err.Error())
			}

			if viper.GetBool("quiet") { // if quiet is set, only set the log to the file
				log.SetOutput(o)
			} else { // else set it to both stdout and the file
				mw := io.MultiWriter(os.Stdout, o)
				log.SetOutput(mw)
			}
		} else { // no logfile
			if viper.GetBool("quiet") { // quiet is enabled so discard all logging
				log.SetOutput(io.Discard)
			} else { // default to stdout
				log.SetOutput(os.Stdout)
			}
		}
	*/

	v := common.GetVersion()
	log.Infof("kairos-agent version %s", v)
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
