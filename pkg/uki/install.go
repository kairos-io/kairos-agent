package uki

import (
	"fmt"
	"io"
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
	"github.com/sanity-io/litter"
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

	// Copy the efi file into the proper dir
	_, err = e.DumpSource(i.spec.Partitions.EFI.MountPoint, i.spec.Active.Source)
	if err != nil {
		return err
	}

	// Remove entries
	// Read all confs
	i.cfg.Logger.Debugf("Parsing efi partition files (skip SkipEntries, replace placeholders etc)")
	err = fsutils.WalkDirFs(i.cfg.Fs, filepath.Join(i.spec.Partitions.EFI.MountPoint), func(path string, info os.DirEntry, err error) error {
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

	for _, entry := range []string{"active", "passive", "recovery"} {
		if err = installArtifactSet(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint, entry, i.cfg.Logger); err != nil {
			return fmt.Errorf("installing artifact set %s: %w", entry, err)
		}
	}

	loaderConfPath := filepath.Join(i.spec.Partitions.EFI.MountPoint, "loader", "loader.conf")
	if err = replacePlaceholders(loaderConfPath, "default", "active", i.cfg.Logger); err != nil {
		return err
	}

	if err = removeArtifactSet(i.cfg.Fs, i.spec.Partitions.EFI.MountPoint); err != nil {
		return fmt.Errorf("removing artifact set: %w", err)
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
func copy(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		panic(err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		panic(err)
	}
	defer destinationFile.Close()

	if _, err = io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}

	// Flushes any buffered data to the destination file
	if err = destinationFile.Sync(); err != nil {
		return err
	}

	if err = sourceFile.Close(); err != nil {
		return err
	}

	return destinationFile.Close()
}

// copy the source file but ranme the base name to as
func copyArtifact(source string, as string) (string, error) {
	newName := strings.ReplaceAll(source, "artifact", as)
	return newName, copy(source, newName)
}

func removeArtifactSet(fs v1.FS, artifactDir string) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if !info.IsDir() && strings.HasPrefix(info.Name(), "artifact") {
			return os.Remove(path)
		}

		return nil
	})
}

func installArtifactSet(fs v1.FS, artifactDir, as string, logger v1.Logger) error {
	return fsutils.WalkDirFs(fs, artifactDir, func(path string, info os.DirEntry, err error) error {
		if !strings.HasPrefix(info.Name(), "artifact") {
			return nil
		}

		newPath, err := copyArtifact(path, as)
		if err != nil {
			return fmt.Errorf("copying artifact from %s to %s: %w", path, newPath, err)
		}
		if strings.HasSuffix(path, ".conf") {
			if err := replacePlaceholders(newPath, "efi", as, logger); err != nil {
				return err
			}
		}

		return nil
	})
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

func (i *InstallAction) replaceFilenamePlaceholder(path, replaceString string) (err error) {
	newName := strings.ReplaceAll(path, "artifact", replaceString)

	return os.Rename(path, newName)
}

func replacePlaceholders(path, key, replaceString string, logger v1.Logger) (err error) {
	// Extract the values
	conf, err := sdkutils.SystemdBootConfReader(path)
	if err != nil {
		logger.Errorf("Error reading conf file %s: %s", path, err)
		return err
	}
	logger.Debugf("Conf file %s has values %v", path, litter.Sdump(conf))

	_, hasKey := conf[key]
	if !hasKey {
		return fmt.Errorf("no %s entry in .conf file", key)
	}

	conf[key] = strings.ReplaceAll(conf[key], "artifact", replaceString)
	newContents := ""
	for k, v := range conf {
		newContents = fmt.Sprintf("%s%s %s\n", newContents, k, v)
	}
	logger.Debugf("Conf file %s new values %v", path, litter.Sdump(conf))

	return os.WriteFile(path, []byte(newContents), os.ModePerm)
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
