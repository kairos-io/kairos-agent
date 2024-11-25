package uki

import (
	"fmt"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
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
	if err = elementalUtils.RunStage(r.cfg, "kairos-uki-reset.pre"); err != nil {
		r.cfg.Logger.Errorf("running kairos-uki-reset.pre stage: %s", err.Error())
	}
	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.reset.pre.hook"); err != nil {
		r.cfg.Logger.Errorf("running kairos-uki-reset.pre hook script: %s", err.Error())
	}

	e := elemental.NewElemental(r.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	// Unmount partitions if any is already mounted before formatting
	err = e.UnmountPartitions(r.spec.Partitions.PartitionsByMountPoint(true))
	if err != nil {
		r.cfg.Logger.Errorf("unmounting partitions: %s", err.Error())
		return err
	}

	// Reformat persistent partition
	if r.spec.FormatPersistent {
		persistent := r.spec.Partitions.Persistent
		if persistent != nil {
			err = e.FormatPartition(persistent)
			if err != nil {
				r.cfg.Logger.Errorf("formatting persistent partition: %s", err.Error())
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
				r.cfg.Logger.Errorf("formatting OEM partition: %s", err.Error())
				return err
			}
		}
	}

	// REMOUNT /efi as RW (its RO by default)
	umount, err := e.MountRWPartition(r.spec.Partitions.EFI)
	if err != nil {
		r.cfg.Logger.Errorf("mounting EFI partition as RW: %s", err.Error())
		return err
	}
	cleanup.Push(umount)

	// Copy "recovery" to "active"
	err = overwriteArtifactSetRole(r.cfg.Fs, constants.UkiEfiDir, "recovery", "active", r.cfg.Logger)
	if err != nil {
		r.cfg.Logger.Errorf("copying recovery to active: %s", err.Error())
		return fmt.Errorf("copying recovery to active: %w", err)
	}

	// Add boot assessment to files by appending +3 to the name
	err = elementalUtils.AddBootAssessment(r.cfg.Fs, r.spec.Partitions.EFI.MountPoint, r.cfg.Logger)
	if err != nil {
		r.cfg.Logger.Warnf("adding boot assesment: %s", err.Error())
	}

	// SelectBootEntry sets the default boot entry to the selected entry
	err = action.SelectBootEntry(r.cfg, "cos")
	// Should we fail? Or warn?
	if err != nil {
		r.cfg.Logger.Errorf("selecting boot entry : %s", err.Error())
		return err
	}

	if mnt, err := elementalUtils.IsMounted(r.cfg, r.spec.Partitions.OEM); !mnt && err == nil {
		err = e.MountPartition(r.spec.Partitions.OEM)
		if err != nil {
			r.cfg.Logger.Errorf("mounting oem partition: %s", err.Error())
			return err
		}
	}

	err = Hook(r.cfg, constants.AfterResetHook)
	if err != nil {
		r.cfg.Logger.Errorf("running after install hook: %s", err.Error())
		return err
	}

	if err = elementalUtils.RunStage(r.cfg, "kairos-uki-reset.after"); err != nil {
		r.cfg.Logger.Errorf("running kairos-uki-reset.after stage: %s", err.Error())
	}

	if err = events.RunHookScript("/usr/bin/kairos-agent.uki.reset.after.hook"); err != nil {
		r.cfg.Logger.Errorf("running kairos-uki-reset.after hook script: %s", err.Error())
	}

	// Do not reboot/poweroff on cleanup errors
	err = cleanup.Cleanup(err)
	if err != nil {
		r.cfg.Logger.Errorf("running cleanup: %s", err.Error())
		return err
	}
	return nil
}
