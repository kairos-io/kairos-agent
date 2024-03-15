package uki

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/utils"
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
	if err = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.pre"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.pre stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.pre.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.pre hook script: %s", err.Error())
	}

	// REMOUNT /efi as RW (its RO by default)
	umount, err := e.MountRWPartition(i.spec.EfiPartition)
	if err != nil {
		i.cfg.Logger.Errorf("remounting efi as RW: %s", err.Error())
		return err
	}
	cleanup.Push(umount)

	// TODO: Get the size of the efi partition and decide if the images can fit
	// before trying the upgrade.
	// If we decide to first copy and then rotate, we need ~4 times the size of
	// the artifact set [TBD]

	// When upgrading recovery, we don't want to replace loader.conf or any other
	// files, thus we take a simpler approach and only install the new efi file
	// and the relevant conf
	if i.spec.RecoveryUpgrade {
		return i.installRecovery()
	}

	// Dump artifact to efi dir
	_, err = e.DumpSource(constants.UkiEfiDir, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping the source: %s", err.Error())
		return err
	}

	// Rotate first
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, "active", "passive", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("rotating active to passive: %s", err.Error())
		return fmt.Errorf("rotating active to passive: %w", err)
	}

	// Install the new artifacts as "active"
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole, "active", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("installing the new artifacts as active: %s", err.Error())
		return fmt.Errorf("installing the new artifacts as active: %w", err)
	}

	loaderConfPath := filepath.Join(constants.UkiEfiDir, "loader", "loader.conf")
	if err = replaceRoleInKey(loaderConfPath, "default", UnassignedArtifactRole, "active", i.cfg.Logger); err != nil {
		i.cfg.Logger.Errorf("replacing role in key: %s", err.Error())
		return err
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole); err != nil {
		i.cfg.Logger.Errorf("removing artifact set: %s", err.Error())
		return fmt.Errorf("removing artifact set: %w", err)
	}

	// SelectBootEntry sets the default boot entry to the selected entry
	err = action.SelectBootEntry(i.cfg, "active")
	// Should we fail? Or warn?
	if err != nil {
		i.cfg.Logger.Errorf("selecting boot entry: %s", err.Error())
		return err
	}

	if err = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.after stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook"); err != nil {
		i.cfg.Logger.Errorf("running kairos-uki-upgrade.after hook script: %s", err.Error())
	}

	return nil
}

// installRecovery replaces the "recovery" role efi and conf files with
// the UnassignedArtifactRole efi and loader files from dir
func (i *UpgradeAction) installRecovery() error {
	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		i.cfg.Logger.Errorf("creating a tmp dir: %s", err.Error())
		return fmt.Errorf("creating a tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Dump artifact to tmp dir
	e := elemental.NewElemental(i.cfg)
	_, err = e.DumpSource(tmpDir, i.spec.Active.Source)
	if err != nil {
		i.cfg.Logger.Errorf("dumping the source to the tmp dir: %s", err.Error())
		return err
	}

	err = copyFile(
		filepath.Join(tmpDir, "EFI", "kairos", UnassignedArtifactRole+".efi"),
		filepath.Join(constants.UkiEfiDir, "EFI", "kairos", "recovery.efi"))
	if err != nil {
		i.cfg.Logger.Errorf("copying efi files: %s", err.Error())
		return err
	}

	targetConfPath := filepath.Join(constants.UkiEfiDir, "loader", "entries", "recovery.conf")
	err = copyFile(
		filepath.Join(tmpDir, "loader", "entries", UnassignedArtifactRole+".conf"),
		targetConfPath)
	if err != nil {
		i.cfg.Logger.Errorf("copying conf files: %s", err.Error())
		return err
	}
	err = replaceRoleInKey(targetConfPath, "efi", UnassignedArtifactRole, "recovery", i.cfg.Logger)
	if err != nil {
		i.cfg.Logger.Errorf("replacing role in in key %s: %s", "efi", err.Error())
		return err
	}

	err = replaceConfTitle(targetConfPath, "recovery")
	if err != nil {
		i.cfg.Logger.Errorf("replacing conf title: %s", err.Error())
		return err
	}

	return nil
}
