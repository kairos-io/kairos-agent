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
	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.pre.hook")

	// REMOUNT /efi as RW (its RO by default)
	umount, err := e.MountRWPartition(i.spec.EfiPartition)
	if err != nil {
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
	_, err = e.DumpSource(constants.XbootloaderDir, i.spec.Active.Source)
	if err != nil {
		return err
	}

	// Rotate first
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.XbootloaderDir, "active", "passive", i.cfg.Logger)
	if err != nil {
		return fmt.Errorf("rotating active to passive: %w", err)
	}

	// Install the new artifacts as "active"
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.XbootloaderDir, UnassignedArtifactRole, "active", i.cfg.Logger)
	if err != nil {
		return fmt.Errorf("installing the new artifacts as active: %w", err)
	}

	loaderConfPath := filepath.Join(constants.UkiEfiDir, "loader", "loader.conf")
	if err = replaceRoleInKey(loaderConfPath, "default", UnassignedArtifactRole, "active", i.cfg.Logger); err != nil {
		return err
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, constants.XbootloaderDir, UnassignedArtifactRole); err != nil {
		return fmt.Errorf("removing artifact set: %w", err)
	}

	// SelectBootEntry sets the default boot entry to the selected entry
	err = action.SelectBootEntry(i.cfg, "active")
	// Should we fail? Or warn?
	if err != nil {
		return err
	}

	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook") //nolint:errcheck

	return nil
}

// installRecovery replaces the "recovery" role efi and conf files with
// the UnassignedArtifactRole efi and loader files from dir
func (i *UpgradeAction) installRecovery() error {
	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("creating a tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Dump artifact to tmp dir
	e := elemental.NewElemental(i.cfg)
	_, err = e.DumpSource(tmpDir, i.spec.Active.Source)
	if err != nil {
		return err
	}

	err = copyFile(
		filepath.Join(tmpDir, "EFI", "kairos", UnassignedArtifactRole+".efi"),
		filepath.Join(constants.XbootloaderDir, "EFI", "kairos", "recovery.efi"))
	if err != nil {
		return err
	}

	targetConfPath := filepath.Join(constants.XbootloaderDir, "loader", "entries", "recovery.conf")
	err = copyFile(
		filepath.Join(tmpDir, "loader", "entries", UnassignedArtifactRole+".conf"),
		targetConfPath)
	if err != nil {
		return err
	}

	return replaceRoleInKey(targetConfPath, "efi", UnassignedArtifactRole, "recovery", i.cfg.Logger)
}
