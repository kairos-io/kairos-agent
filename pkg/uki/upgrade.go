package uki

import (
	"fmt"
	"path/filepath"

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

	// Dump artifact to efi dir
	_, err = e.DumpSource(constants.UkiEfiDir, i.spec.Active.Source)
	if err != nil {
		return err
	}

	// Rotate first
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, "active", "passive", i.cfg.Logger)
	if err != nil {
		return fmt.Errorf("rotating active to passive: %w", err)
	}

	// Install the new artifacts as "active"
	err = overwriteArtifactSetRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole, "active", i.cfg.Logger)
	if err != nil {
		return fmt.Errorf("installing the new artifacts as active: %w", err)
	}

	loaderConfPath := filepath.Join(constants.UkiEfiDir, "loader", "loader.conf")
	if err = replaceRoleInKey(loaderConfPath, "default", UnassignedArtifactRole, "active", i.cfg.Logger); err != nil {
		return err
	}

	if err = removeArtifactSetWithRole(i.cfg.Fs, constants.UkiEfiDir, UnassignedArtifactRole); err != nil {
		return fmt.Errorf("removing artifact set: %w", err)
	}

	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook") //nolint:errcheck

	return nil
}
