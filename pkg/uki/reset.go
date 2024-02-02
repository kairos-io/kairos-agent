package uki

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/utils"
)

type ResetAction struct {
	cfg  *config.Config
	spec *v1.ResetUkiSpec
}

func NewResetAction(cfg *config.Config, spec *v1.ResetUkiSpec) *ResetAction {
	return &ResetAction{cfg: cfg, spec: spec}
}

func (r *ResetAction) Run() (err error) {
	// Run pre-install stage
	_ = elementalUtils.RunStage(r.cfg, "kairos-uki-reset.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.reset.pre.hook")

	e := elemental.NewElemental(r.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	// Unmount partitions if any is already mounted before formatting
	err = e.UnmountPartitions(r.spec.Partitions.PartitionsByMountPoint(true))
	if err != nil {
		return err
	}

	// Reformat persistent partition
	if r.spec.FormatPersistent {
		persistent := r.spec.Partitions.Persistent
		if persistent != nil {
			err = e.FormatPartition(persistent)
			if err != nil {
				return err
			}
		}
	}

	// Reformat OEM
	if r.spec.FormatOEM {
		oem := r.spec.Partitions.OEM
		if oem != nil {
			err = e.FormatPartition(oem)
			if err != nil {
				return err
			}
		}
	}

	_ = elementalUtils.RunStage(r.cfg, "kairos-uki-reset.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.reset.after.hook") //nolint:errcheck

	// Do not reboot/poweroff on cleanup errors
	err = cleanup.Cleanup(err)
	if err != nil {
		return err
	}
	return nil
}
