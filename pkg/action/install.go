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
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	events "github.com/kairos-io/kairos-sdk/bus"
	"path/filepath"
	"strings"
	"time"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
)

func (i *InstallAction) installHook(hook string, chroot bool) error {
	if chroot {
		extraMounts := map[string]string{}
		persistent := i.spec.Partitions.Persistent
		if persistent != nil && persistent.MountPoint != "" {
			extraMounts[persistent.MountPoint] = cnst.UsrLocalPath
		}
		oem := i.spec.Partitions.OEM
		if oem != nil && oem.MountPoint != "" {
			extraMounts[oem.MountPoint] = cnst.OEMPath
		}
		return ChrootHook(i.cfg, hook, i.spec.Active.MountPoint, extraMounts)
	}
	return Hook(i.cfg, hook)
}

func (i *InstallAction) createInstallStateYaml(sysMeta, recMeta interface{}) error {
	if i.spec.Partitions.State == nil || i.spec.Partitions.Recovery == nil {
		return fmt.Errorf("undefined state or recovery partition")
	}

	// If recovery image is a copyied file from active reuse the same source and metadata
	recSource := i.spec.Recovery.Source
	if i.spec.Recovery.Source.IsFile() && i.spec.Active.File == i.spec.Recovery.Source.Value() {
		recMeta = sysMeta
		recSource = i.spec.Active.Source
	}

	installState := &v1.InstallState{
		Date: time.Now().Format(time.RFC3339),
		Partitions: map[string]*v1.PartitionState{
			cnst.StatePartName: {
				FSLabel: i.spec.Partitions.State.FilesystemLabel,
				Images: map[string]*v1.ImageState{
					cnst.ActiveImgName: {
						Source:         i.spec.Active.Source,
						SourceMetadata: sysMeta,
						Label:          i.spec.Active.Label,
						FS:             i.spec.Active.FS,
					},
					cnst.PassiveImgName: {
						Source:         i.spec.Active.Source,
						SourceMetadata: sysMeta,
						Label:          i.spec.Passive.Label,
						FS:             i.spec.Passive.FS,
					},
				},
			},
			cnst.RecoveryPartName: {
				FSLabel: i.spec.Partitions.Recovery.FilesystemLabel,
				Images: map[string]*v1.ImageState{
					cnst.RecoveryImgName: {
						Source:         recSource,
						SourceMetadata: recMeta,
						Label:          i.spec.Recovery.Label,
						FS:             i.spec.Recovery.FS,
					},
				},
			},
		},
	}
	if i.spec.Partitions.OEM != nil {
		installState.Partitions[cnst.OEMPartName] = &v1.PartitionState{
			FSLabel: i.spec.Partitions.OEM.FilesystemLabel,
		}
	}
	if i.spec.Partitions.Persistent != nil {
		installState.Partitions[cnst.PersistentPartName] = &v1.PartitionState{
			FSLabel: i.spec.Partitions.Persistent.FilesystemLabel,
		}
	}

	return i.cfg.WriteInstallState(
		installState,
		filepath.Join(i.spec.Partitions.State.MountPoint, cnst.InstallStateFile),
		filepath.Join(i.spec.Partitions.Recovery.MountPoint, cnst.InstallStateFile),
	)
}

type InstallAction struct {
	cfg  *config.Config
	spec *v1.InstallSpec
}

func NewInstallAction(cfg *config.Config, spec *v1.InstallSpec) *InstallAction {
	return &InstallAction{cfg: cfg, spec: spec}
}

// Run will install the system from a given configuration
func (i InstallAction) Run() (err error) {
	e := elemental.NewElemental(i.cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	// Run pre-install stage
	_ = utils.RunStage(i.cfg, "kairos-install.pre")
	events.RunHookScript("/usr/bin/kairos-agent.install.pre.hook") //nolint:errcheck

	// Set installation sources from a downloaded ISO
	if i.spec.Iso != "" {
		tmpDir, err := e.GetIso(i.spec.Iso)
		if err != nil {
			return err
		}
		cleanup.Push(func() error { return i.cfg.Fs.RemoveAll(tmpDir) })
		err = e.UpdateSourcesFormDownloadedISO(tmpDir, &i.spec.Active, &i.spec.Recovery)
		if err != nil {
			return err
		}
	}

	// Check no-format flag
	if i.spec.NoFormat {
		// Check force flag against current device
		labels := []string{i.spec.Active.Label, i.spec.Recovery.Label}
		if e.CheckActiveDeployment(labels) && !i.spec.Force {
			return fmt.Errorf("use `force` flag to run an installation over the current running deployment")
		}
	} else {
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
	}

	err = e.MountPartitions(i.spec.Partitions.PartitionsByMountPoint(false))
	if err != nil {
		return err
	}
	cleanup.Push(func() error {
		return e.UnmountPartitions(i.spec.Partitions.PartitionsByMountPoint(true))
	})

	// Before install hook happens after partitioning but before the image OS is applied
	err = i.installHook(cnst.BeforeInstallHook, false)
	if err != nil {
		return err
	}

	// Deploy active image
	systemMeta, err := e.DeployImage(&i.spec.Active, true)
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return e.UnmountImage(&i.spec.Active) })

	// Create extra dirs in rootfs as afterwards this will be impossible due to RO system
	createExtraDirsInRootfs(i.cfg, i.spec.ExtraDirsRootfs, i.spec.Active.MountPoint)

	// Copy cloud-init if any
	err = e.CopyCloudConfig(i.spec.CloudInit)
	if err != nil {
		return err
	}
	// Install grub
	grub := utils.NewGrub(i.cfg)
	err = grub.Install(
		i.spec.Target,
		i.spec.Active.MountPoint,
		i.spec.Partitions.State.MountPoint,
		i.spec.GrubConf,
		i.spec.Tty,
		i.spec.Firmware == v1.EFI,
		i.spec.Partitions.State.FilesystemLabel,
	)
	if err != nil {
		return err
	}

	// Relabel SELinux
	binds := map[string]string{}
	if mnt, _ := utils.IsMounted(i.cfg, i.spec.Partitions.Persistent); mnt {
		binds[i.spec.Partitions.Persistent.MountPoint] = cnst.UsrLocalPath
	}
	if mnt, _ := utils.IsMounted(i.cfg, i.spec.Partitions.OEM); mnt {
		binds[i.spec.Partitions.OEM.MountPoint] = cnst.OEMPath
	}
	err = utils.ChrootedCallback(
		i.cfg, i.spec.Active.MountPoint, binds, func() error { return e.SelinuxRelabel("/", true) },
	)
	if err != nil {
		return err
	}

	err = i.installHook(cnst.AfterInstallChrootHook, true)
	if err != nil {
		return err
	}

	// Installation rebrand (only grub for now)
	err = e.SetDefaultGrubEntry(
		i.spec.Partitions.State.MountPoint,
		i.spec.Active.MountPoint,
		i.spec.GrubDefEntry,
	)
	if err != nil {
		return err
	}

	// Unmount active image
	err = e.UnmountImage(&i.spec.Active)
	if err != nil {
		return err
	}
	// Install Recovery
	recoveryMeta, err := e.DeployImage(&i.spec.Recovery, false)
	if err != nil {
		return err
	}
	// Install Passive
	_, err = e.DeployImage(&i.spec.Passive, false)
	if err != nil {
		return err
	}

	err = i.installHook(cnst.AfterInstallHook, false)
	if err != nil {
		return err
	}

	// Add state.yaml file on state and recovery partitions
	err = i.createInstallStateYaml(systemMeta, recoveryMeta)
	if err != nil {
		return err
	}

	// Do not reboot/poweroff on cleanup errors
	err = cleanup.Cleanup(err)
	if err != nil {
		return err
	}

	// If we want to eject the cd, create the required executable so the cd is ejected at shutdown
	out, _ := i.cfg.Runner.Run("cat", "/proc/cmdline")
	bootedFromCD := strings.Contains(string(out), "cdroot")

	if i.cfg.EjectCD && bootedFromCD {
		i.cfg.Logger.Infof("Writing eject script")
		err = i.cfg.Fs.WriteFile("/usr/lib/systemd/system-shutdown/eject", []byte(cnst.EjectScript), 0744)
		if err != nil {
			i.cfg.Logger.Warnf("Could not write eject script, cdrom wont be ejected automatically: %s", err)
		}
	}

	_ = utils.RunStage(i.cfg, "kairos-install.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.install.after.hook") //nolint:errcheck

	return hook.Run(*i.cfg, i.spec, hook.AfterInstall...)
}
