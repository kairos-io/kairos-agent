package uki

import (
	"github.com/Masterminds/semver/v3"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/sanity-io/litter"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type UpgradeAction struct {
	cfg  *config.Config
	spec *v1.UpgradeUkiSpec
}

func NewUpgradeAction(cfg *config.Config, spec *v1.UpgradeUkiSpec) *UpgradeAction {
	return &UpgradeAction{cfg: cfg, spec: spec}
}

func (i *UpgradeAction) Run() (err error) {
	e := elemental.NewElemental(i.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Run pre-install stage
	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.pre.hook")

	// REMOUNT /efi as RW (its RO by default)
	umount, err := e.MountRWPartition(i.spec.EfiPartition)
	if err != nil {
		return err
	}
	cleanup.Push(umount)
	// TODO: Check number of existing UKI files
	efiFiles, err := i.getEfiFiles()
	if err != nil {
		return err
	}
	i.cfg.Logger.Infof("Found %d UKI files", len(efiFiles))
	if len(efiFiles) > i.cfg.UkiMaxEntries && i.cfg.UkiMaxEntries > 0 {
		i.cfg.Logger.Infof("Found %d UKI files, which is over max entries allowed(%d) removing the oldest one", len(efiFiles), i.cfg.UkiMaxEntries)
		versionList := semver.Collection{}
		for _, f := range efiFiles {
			versionList = append(versionList, semver.MustParse(f))
		}
		// Sort it so the oldest one is first
		sort.Sort(versionList)
		i.cfg.Logger.Debugf("All versions found: %s", litter.Sdump(versionList))
		// Remove the oldest one
		i.cfg.Logger.Infof("Removing: %s", filepath.Join(i.spec.EfiPartition.MountPoint, "EFI", "kairos", versionList[0].Original()))
		err = i.cfg.Fs.Remove(filepath.Join(i.spec.EfiPartition.MountPoint, "EFI", "kairos", versionList[0].Original()))
		if err != nil {
			return err
		}
		// Remove the conf file as well
		i.cfg.Logger.Infof("Removing: %s", filepath.Join(i.spec.EfiPartition.MountPoint, "loader", "entries", versionList[0].String()+".conf"))
		// Don't care about errors here, systemd-boot will ignore any configs if it cant find the efi file mentioned in it
		e := i.cfg.Fs.Remove(filepath.Join(i.spec.EfiPartition.MountPoint, "loader", "entries", versionList[0].String()+".conf"))
		if e != nil {
			i.cfg.Logger.Warnf("Failed to remove conf file: %s", e)
		}
	} else {
		i.cfg.Logger.Infof("Found %d UKI files, which is under max entries allowed(%d) not removing any", len(efiFiles), i.cfg.UkiMaxEntries)
	}

	// Dump artifact to efi dir
	_, err = e.DumpSource(constants.UkiEfiDir, i.spec.Active.Source)
	if err != nil {
		return err
	}

	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook") //nolint:errcheck

	return nil
}

func (i *UpgradeAction) getEfiFiles() ([]string, error) {
	var efiFiles []string
	files, err := os.ReadDir(filepath.Join(i.spec.EfiPartition.MountPoint, "EFI", "kairos"))
	if err != nil {
		return efiFiles, err
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".efi") {
			efiFiles = append(efiFiles, file.Name())
		}
	}
	return efiFiles, nil
}
