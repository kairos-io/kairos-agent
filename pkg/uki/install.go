package uki

import (
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	events "github.com/kairos-io/kairos-sdk/bus"
	"os"
	"path/filepath"
)

type InstallAction struct {
	cfg  *config.Config
	spec *v1.InstallUkiSpec
}

func NewInstallAction(cfg *config.Config, spec *v1.InstallUkiSpec) *InstallAction {
	return &InstallAction{cfg: cfg, spec: spec}
}

func (i *InstallAction) Run() (err error) {
	e := elemental.NewElemental(i.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Run pre-install stage
	_ = utils.RunStage(i.cfg, "kairos-uki-install.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.install.pre.hook")

	// Get source (from spec?)
	// If source is empty then we need to find the media we booted from....to get the efi files...
	// cdrom is kind fo easy...
	// we set the label EFI_ISO_BOOT so we look for that and then mount the image inside...
	// TODO: Extract this to a different functions or something. Maybe PrepareUKISource or something
	_ = fsutils.MkdirAll(i.cfg.Fs, constants.UkiCdromSource, os.ModeDir|os.ModePerm)
	_ = fsutils.MkdirAll(i.cfg.Fs, constants.UkiSource, os.ModeDir|os.ModePerm)

	cdRom := &v1.Partition{
		FilesystemLabel: "UKI_ISO_INSTALL", // TODO: Hardcoded on ISO creation
		FS:              "iso9660",
		Path:            "/dev/disk/by-label/UKI_ISO_INSTALL",
		MountPoint:      constants.UkiCdromSource,
	}
	err = e.MountPartition(cdRom)

	if err != nil {
		return err
	}
	cleanup.Push(func() error {
		return e.UnmountPartition(cdRom)
	})

	// TODO: hardcoded
	image := &v1.Image{
		File:       "/run/install/cdrom/efiboot.img", // TODO: Hardcoded on ISO creation
		Label:      "UKI_SOURCE",                     // Made up, only for logging
		MountPoint: constants.UkiSource,
	}

	err = e.MountImage(image)
	if err != nil {
		return err
	}
	cleanup.Push(func() error {
		return e.UnmountImage(image)
	})

	// Create EFI partition (fat32), we already create the efi partition on normal efi install,we can reuse that?
	// Create COS_OEM/COS_PERSISTANT if set (optional)
	// I guess we need to set sensible default values here for sizes? oem -> 64Mb as usual but if no persistent then EFI max size?
	// if persistent then EFI = source size * 2 (or maybe 3 times! so we can upgrade!) and then persistent the rest of the disk?

	// Deactivate any active volume on target
	err = e.DeactivateDevices()
	if err != nil {
		return err
	}
	// Partition device
	err = e.PartitionAndFormatDevice(i.spec)
	if err != nil {
		return err
	}

	err = e.MountPartitions(i.spec.GetPartitions().PartitionsByMountPoint(false))
	if err != nil {
		return err
	}
	cleanup.Push(func() error {
		return e.UnmountPartitions(i.spec.GetPartitions().PartitionsByMountPoint(true))
	})

	// Before install hook happens after partitioning but before the image OS is applied
	//err = i.installHook(cnst.BeforeInstallHook, false)
	//if err != nil {
	//	return err
	//}

	// Store cloud-config in TPM or copy it to COS_OEM?
	// Create dir structure
	//  - /EFI/Kairos/ -> Store our older efi images ?
	//  - /EFI/BOOT/ -> Default fallback dir (efi search for bootaa64.efi or bootx64.efi if no entries in the boot manager)

	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.EfiDir, "EFI", "BOOT"), constants.DirPerm)
	if err != nil {
		return err
	}

	// Copy the efi file into the proper dir
	source := v1.NewDirSrc(constants.UkiSource)
	_, err = e.DumpSource(i.spec.Partitions.EFI.MountPoint, source)
	if err != nil {
		return err
	}
	// Remove all boot manager entries?
	// Create boot manager entry
	// Set default entry to the one we just created
	// Probably copy efi utils, like the Mokmanager and even the shim or grub efi to help with troubleshooting?
	_ = utils.RunStage(i.cfg, "kairos-uki-install.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.install.after.hook") //nolint:errcheck

	return hook.Run(*i.cfg, i.spec, hook.AfterUkiInstall...)
}
