/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package action

import (
	"fmt"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"path/filepath"
	"time"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
)

func (r *ResetAction) resetHook(hook string, chroot bool) error {
	if chroot {
		extraMounts := map[string]string{}
		persistent := r.spec.Partitions.Persistent
		if persistent != nil && persistent.MountPoint != "" {
			extraMounts[persistent.MountPoint] = cnst.UsrLocalPath
		}
		oem := r.spec.Partitions.OEM
		if oem != nil && oem.MountPoint != "" {
			extraMounts[oem.MountPoint] = cnst.OEMPath
		}
		return ChrootHook(r.cfg, hook, r.spec.Active.MountPoint, extraMounts)
	}
	return Hook(r.cfg, hook)
}

type ResetAction struct {
	cfg  *agentConfig.Config
	spec *v1.ResetSpec
}

func NewResetAction(cfg *agentConfig.Config, spec *v1.ResetSpec) *ResetAction {
	return &ResetAction{cfg: cfg, spec: spec}
}

func (r *ResetAction) updateInstallState(e *elemental.Elemental, cleanup *utils.CleanStack, meta interface{}) error {
	if r.spec.Partitions.Recovery == nil || r.spec.Partitions.State == nil {
		return fmt.Errorf("undefined state or recovery partition")
	}

	installState := &v1.InstallState{
		Date: time.Now().Format(time.RFC3339),
		Partitions: map[string]*v1.PartitionState{
			cnst.StatePartName: {
				FSLabel: r.spec.Partitions.State.FilesystemLabel,
				Images: map[string]*v1.ImageState{
					cnst.ActiveImgName: {
						Source:         r.spec.Active.Source,
						SourceMetadata: meta,
						Label:          r.spec.Active.Label,
						FS:             r.spec.Active.FS,
					},
					cnst.PassiveImgName: {
						Source:         r.spec.Active.Source,
						SourceMetadata: meta,
						Label:          r.spec.Passive.Label,
						FS:             r.spec.Passive.FS,
					},
				},
			},
		},
	}
	if r.spec.Partitions.OEM != nil {
		installState.Partitions[cnst.OEMPartName] = &v1.PartitionState{
			FSLabel: r.spec.Partitions.OEM.FilesystemLabel,
		}
	}
	if r.spec.Partitions.Persistent != nil {
		installState.Partitions[cnst.PersistentPartName] = &v1.PartitionState{
			FSLabel: r.spec.Partitions.Persistent.FilesystemLabel,
		}
	}
	if r.spec.State != nil && r.spec.State.Partitions != nil {
		installState.Partitions[cnst.RecoveryPartName] = r.spec.State.Partitions[cnst.RecoveryPartName]
	}

	umount, err := e.MountRWPartition(r.spec.Partitions.Recovery)
	if err != nil {
		return err
	}
	cleanup.Push(umount)

	return r.cfg.WriteInstallState(
		installState,
		filepath.Join(r.spec.Partitions.State.MountPoint, cnst.InstallStateFile),
		filepath.Join(r.spec.Partitions.Recovery.MountPoint, cnst.InstallStateFile),
	)
}

// ResetRun will reset the cos system to by following several steps
func (r ResetAction) Run() (err error) {
	e := elemental.NewElemental(r.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	// Reformat state partition
	// We should expose this under a flag, to reformat state before starting
	// In case state fs is broken somehow
	// We used to format it blindly but if something happens between this format and the new image copy down below
	// You end up with a broken system with no active or passive and reset may not even work at that point
	// TODO: Either create a flag to do this or ignore it and never format the partition
	/*
		if r.spec.FormatState {
			state := r.spec.Partitions.State
			if state != nil {
				err = e.UnmountPartition(state)
				if err != nil {
					return err
				}
				err = e.FormatPartition(state)
				if err != nil {
					return err
				}
				// Mount it back
				err = e.MountPartition(state)
				if err != nil {
					return err
				}
			}
		}
	*/

	// Reformat persistent partition
	if r.spec.FormatPersistent {
		persistent := r.spec.Partitions.Persistent
		if persistent != nil {
			err = e.UnmountPartition(persistent)
			if err != nil {
				return err
			}
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
			// Try to umount
			err = e.UnmountPartition(oem)
			if err != nil {
				return err
			}
			err = e.FormatPartition(oem)
			if err != nil {
				return err
			}
			// Mount it back, as oem is mounted during recovery, keep everything as is
			err = e.MountPartition(oem)
			if err != nil {
				return err
			}
		}
	}

	// Before reset hook happens once partitions are aready and before deploying the OS image
	err = r.resetHook(cnst.BeforeResetHook, false)
	if err != nil {
		return err
	}

	// Mount COS_STATE so we can write the new images
	err = e.MountPartition(r.spec.Partitions.State)
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return e.UnmountPartition(r.spec.Partitions.State) })

	// Deploy active image
	meta, err := e.DeployImage(&r.spec.Active, true)
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return e.UnmountImage(&r.spec.Active) })

	// Create extra dirs in rootfs as afterwards this will be impossible due to RO system
	createExtraDirsInRootfs(r.cfg, r.spec.ExtraDirsRootfs, r.spec.Active.MountPoint)

	// Mount EFI partition before installing grub as under EFI this copies stuff in there
	if r.spec.Efi {
		err = e.MountPartition(r.spec.Partitions.EFI)
		if err != nil {
			return err
		}
		cleanup.Push(func() error { return e.UnmountPartition(r.spec.Partitions.EFI) })
	}
	//TODO: does bios needs to be mounted here?

	// install grub
	grub := utils.NewGrub(r.cfg)
	err = grub.Install(
		r.spec.Target,
		r.spec.Active.MountPoint,
		r.spec.Partitions.State.MountPoint,
		r.spec.GrubConf,
		r.spec.Tty,
		r.spec.Efi,
		r.spec.Partitions.State.FilesystemLabel,
	)
	if err != nil {
		return err
	}

	// Relabel SELinux
	// TODO probably relabelling persistent volumes should be an opt in feature, it could
	// have undesired effects in case of failures
	binds := map[string]string{}
	if mnt, _ := utils.IsMounted(r.cfg, r.spec.Partitions.Persistent); mnt {
		binds[r.spec.Partitions.Persistent.MountPoint] = cnst.UsrLocalPath
	}
	if mnt, _ := utils.IsMounted(r.cfg, r.spec.Partitions.OEM); mnt {
		binds[r.spec.Partitions.OEM.MountPoint] = cnst.OEMPath
	}
	err = utils.ChrootedCallback(
		r.cfg, r.spec.Active.MountPoint, binds,
		func() error { return e.SelinuxRelabel("/", true) },
	)
	if err != nil {
		return err
	}

	err = r.resetHook(cnst.AfterResetChrootHook, true)
	if err != nil {
		return err
	}

	// installation rebrand (only grub for now)
	err = e.SetDefaultGrubEntry(
		r.spec.Partitions.State.MountPoint,
		r.spec.Active.MountPoint,
		r.spec.GrubDefEntry,
	)
	if err != nil {
		return err
	}

	// Unmount active image
	err = e.UnmountImage(&r.spec.Active)
	if err != nil {
		return err
	}

	// Install Passive
	_, err = e.DeployImage(&r.spec.Passive, false)
	if err != nil {
		return err
	}

	err = r.resetHook(cnst.AfterResetHook, false)
	if err != nil {
		return err
	}

	err = r.updateInstallState(e, cleanup, meta)
	if err != nil {
		return err
	}

	// Do not reboot/poweroff on cleanup errors
	err = cleanup.Cleanup(err)
	if err != nil {
		return err
	}

	return err
}
