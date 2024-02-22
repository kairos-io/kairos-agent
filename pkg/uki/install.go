package uki

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	events "github.com/kairos-io/kairos-sdk/bus"
	sdkutils "github.com/kairos-io/kairos-sdk/utils"
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

	// Before install hook happens after partitioning but before the image OS is applied (this is for compatibility with normal install, so users can reuse their configs)
	err = Hook(i.cfg, constants.BeforeInstallHook)
	if err != nil {
		return err
	}

	// Store cloud-config in TPM or copy it to COS_OEM?
	// Copy cloud-init if any
	err = e.CopyCloudConfig(i.spec.CloudInit)
	if err != nil {
		return err
	}
	// Create dir structure
	//  - /EFI/Kairos/ -> Store our older efi images ?
	//  - /EFI/BOOT/ -> Default fallback dir (efi search for bootaa64.efi or bootx64.efi if no entries in the boot manager)

	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.EfiDir, "EFI", "BOOT"), constants.DirPerm)
	if err != nil {
		return err
	}
	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.EfiDir, "EFI", "systemd", "drivers"), constants.DirPerm)
	if err != nil {
		return err
	}
	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.EfiDir, "loader", "entries"), constants.DirPerm)
	if err != nil {
		return err
	}
	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.XBOOTLOADERDir, "EFI"), constants.DirPerm)
	if err != nil {
		return err
	}
	err = fsutils.MkdirAll(i.cfg.Fs, filepath.Join(constants.XBOOTLOADERDir, "loader", "entries"), constants.DirPerm)
	if err != nil {
		return err
	}

	// TODO: Check if the size of the files we are going to copy, will fit in the
	// partition. If not stop here.

	// Copy the efi files into the XBOOTLDR dir
	_, err = e.DumpSource(i.spec.Partitions.XBOOTLDR.MountPoint, i.spec.Active.Source)
	if err != nil {
		return err
	}

	// Copy the systemd-boot efi files into the EFI dir
	for _, efiFile := range []string{"/EFI/BOOT/bootaa64.efi", "/EFI/BOOT/BOOTX64.efi"} {
		if yes, _ := fsutils.Exists(i.cfg.Fs, filepath.Join(i.spec.Active.Source.Value(), efiFile)); yes {
			err = copyFile(filepath.Join(i.spec.Active.Source.Value(), efiFile), filepath.Join(i.spec.Partitions.EFI.MountPoint, efiFile))
			if err != nil {
				i.cfg.Logger.Debugf("Error copying efi file %s: %s", efiFile, err)
			}
		} else {
			i.cfg.Logger.Debugf("File %s does not exist", efiFile)
		}
	}

	// Copy drivers for FS into the efi dir
	// download file from https://github.com/pbatard/efifs/releases/download/v1.9/ext2_x64.efi
	// TODO: provide this like the systemd artifacts, as part of the install media via luet package, maybe with the systemd-boot package?
	err = DownloadFile(filepath.Join(i.spec.Partitions.EFI.MountPoint, "EFI", "systemd", "drivers", "ext2_x64.efi"), "https://github.com/pbatard/efifs/releases/download/v1.9/ext2_x64.efi")
	if err != nil {
		return err
	}

	// Copy the loader files into the EFI dir
	err = copyFile(filepath.Join(i.spec.Active.Source.Value(), "/loader/loader.conf"), filepath.Join(i.spec.Partitions.EFI.MountPoint, "/loader/loader.conf"))
	if err != nil {
		i.cfg.Logger.Debugf("Error copying efi file %s: %s", "/loader/loader.conf", err)
	}
	// Remove entries
	// Read all confs
	i.cfg.Logger.Debugf("Parsing efi partition files (skip SkipEntries, replace placeholders etc)")
	err = fsutils.WalkDirFs(i.cfg.Fs, filepath.Join(i.spec.Partitions.XBOOTLDR.MountPoint), func(path string, info os.DirEntry, err error) error {
		filename := info.Name()
		if err != nil {
			i.cfg.Logger.Errorf("Error walking path: %s, %s", filename, err.Error())
			return err
		}

		i.cfg.Logger.Debugf("Checking file %s", path)
		if info.IsDir() {
			return nil
		}

		if filepath.Ext(filename) == ".conf" {
			// Extract the values
			conf, err := sdkutils.SystemdBootConfReader(path)
			if err != nil {
				i.cfg.Logger.Errorf("Error reading conf file to extract values %s: %s", path, err)
				return err
			}
			if len(conf["cmdline"]) == 0 {
				return nil
			}

			// Check if the cmdline matches any of the entries in the skip list
			skip := false
			for _, entry := range i.spec.SkipEntries {
				if strings.Contains(conf["cmdline"], entry) {
					i.cfg.Logger.Debugf("Found match for %s in %s", entry, path)
					skip = true
					break
				}
			}
			if skip {
				return i.SkipEntry(path, conf)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	for _, role := range []string{"active", "passive", "recovery"} {
		if err = copyArtifactSetRole(i.cfg.Fs, i.spec.Partitions.XBOOTLDR.MountPoint, UnassignedArtifactRole, role, i.cfg.Logger); err != nil {
			return fmt.Errorf("installing the new artifact set as %s: %w", role, err)
		}
	}

	loaderConfPath := filepath.Join(i.spec.Partitions.EFI.MountPoint, "loader", "loader.conf")
	if err = replaceRoleInKey(loaderConfPath, "default", UnassignedArtifactRole, "active", i.cfg.Logger); err != nil {
		return err
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, UnassignedArtifactRole); err != nil {
		return fmt.Errorf("removing artifact set with role %s: %w", UnassignedArtifactRole, err)
	}
	if err = removeArtifactSetWithRole(i.cfg.Fs, i.spec.Partitions.XBOOTLDR.MountPoint, UnassignedArtifactRole); err != nil {
		return fmt.Errorf("removing artifact set with role %s: %w", UnassignedArtifactRole, err)
	}

	// after install hook happens after install (this is for compatibility with normal install, so users can reuse their configs)
	err = Hook(i.cfg, constants.AfterInstallHook)
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

func (i *InstallAction) SkipEntry(path string, conf map[string]string) (err error) {
	// If match, get the efi file and remove it
	if conf["efi"] != "" {
		i.cfg.Logger.Debugf("Removing efi file %s", conf["efi"])
		// First remove the efi file
		err = i.cfg.Fs.Remove(filepath.Join(i.spec.Partitions.EFI.MountPoint, conf["efi"]))
		if err != nil {
			i.cfg.Logger.Errorf("Error removing efi file %s: %s", conf["efi"], err)
			return err
		}
		// Then remove the conf file
		i.cfg.Logger.Debugf("Removing conf file %s", path)
		err = i.cfg.Fs.Remove(path)
		if err != nil {
			i.cfg.Logger.Errorf("Error removing conf file %s: %s", path, err)
			return err
		}
		// Do not continue checking the conf file, we already done all we needed
	}
	return err
}

// Hook is RunStage wrapper that only adds logic to ignore errors
// in case v1.Config.Strict is set to false
func Hook(config *config.Config, hook string) error {
	config.Logger.Infof("Running %s hook", hook)
	err := utils.RunStage(config, hook)
	if !config.Strict {
		err = nil
	}
	return err
}

func DownloadFile(filepath string, url string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	return err
}
