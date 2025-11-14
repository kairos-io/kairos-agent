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
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
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

	if i.spec.NoFormat {
		i.cfg.Logger.Infof("NoFormat is true, skipping format and partitioning")
		// Check force flag against current device
		labels := []string{i.spec.Active.Label, i.spec.Recovery.Label}
		if e.CheckActiveDeployment(labels) && !i.spec.Force {
			return fmt.Errorf("use `force` flag to run an installation over the current running deployment")
		}

		if i.spec.Target == "" || i.spec.Target == "auto" {
			// This needs to run after the pre-install stage to give the user the
			// opportunity to prepare the target disk in the pre-install stage.
			device, err := config.DetectPreConfiguredDevice(i.cfg.Logger)
			if err != nil {
				return fmt.Errorf("no target device specified and no device found: %s", err)
			}
			i.cfg.Logger.Infof("No target device specified, using pre-configured device: %s", device)
			i.spec.Target = device
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

	// Check if we should fail the installation by checking the sentinel file FailInstallationFileSentinel
	if toFail, err := utils.CheckFailedInstallation(cnst.FailInstallationFileSentinel); toFail {
		return err
	}

	// Deploy active image
	_, err = e.DeployImage(&i.spec.Active, true)
	if err != nil {
		return err
	}
	cleanup.Push(func() error { return e.UnmountImage(&i.spec.Active) })

	// Create extra dirs in rootfs as afterwards this will be impossible due to RO system
	createExtraDirsInRootfs(i.cfg, i.spec.ExtraDirsRootfs, i.spec.Active.MountPoint)

	// Copy cloud-config if any
	if err = e.CopyCloudConfig(i.spec.CloudInit); err != nil {
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
		i.spec.Firmware == sdkConstants.EFI,
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
	_, err = e.DeployImage(&i.spec.Recovery, false)
	if err != nil {
		return err
	}
	// Install Passive
	_, err = e.DeployImage(&i.spec.Passive, false)
	if err != nil {
		return err
	}

	err = hook.Run(*i.cfg, i.spec, hook.PostInstall...)
	if err != nil {
		return err
	}

	err = i.installHook(cnst.AfterInstallHook, false)
	if err != nil {
		return err
	}

	// Do not reboot/poweroff on cleanup errors
	err = cleanup.Cleanup(err)
	if err != nil {
		return err
	}

	// If we want to eject the cd, create the required executable so the cd is ejected at shutdown
	out, _ := i.cfg.Fs.ReadFile("/proc/cmdline")
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

	return hook.Run(*i.cfg, i.spec, hook.FinishInstall...)
}
