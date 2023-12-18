package uki

import (
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
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
	// TODO: Check size of EFI partition to see if we can upgrade
	// TODO: Check size of source to see if we can upgrade
	// TODO: Check number of existing UKI files
	// TODO: Load them, order them via semver
	// TODO: Remove the latest one if its over the max number of entries
	// Dump artifact to efi dir
	_, err = e.DumpSource(constants.UkiEfiDir, i.spec.Active.Source)
	if err != nil {
		return err
	}
	
	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook") //nolint:errcheck

	return hook.Run(*i.cfg, i.spec, hook.AfterUkiUpgrade...)
}
