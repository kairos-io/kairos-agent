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
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	"github.com/kairos-io/kairos-agent/v2/pkg/partitioner"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/sanity-io/litter"
	"path/filepath"
	"time"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
)

// UpgradeQCS6490Action represents the struct that will run the upgrade from start to finish for a QCS6490 board
type UpgradeQCS6490Action struct {
	config *agentConfig.Config
	spec   *v1.UpgradeSpec
}

func NewUpgradeQCS6490Action(config *agentConfig.Config, spec *v1.UpgradeSpec) *UpgradeQCS6490Action {
	return &UpgradeQCS6490Action{config: config, spec: spec}
}

func (u UpgradeQCS6490Action) Info(s string, args ...interface{}) {
	u.config.Logger.Infof(s, args...)
}

func (u UpgradeQCS6490Action) Debug(s string, args ...interface{}) {
	u.config.Logger.Debugf(s, args...)
}

func (u UpgradeQCS6490Action) Error(s string, args ...interface{}) {
	u.config.Logger.Errorf(s, args...)
}

func (u UpgradeQCS6490Action) upgradeHook(hook string, chroot bool) error {
	u.Info("Applying '%s' hook", hook)
	if chroot {
		extraMounts := map[string]string{}

		oemDevice := u.spec.Partitions.OEM
		if oemDevice != nil && oemDevice.MountPoint != "" {
			extraMounts[oemDevice.MountPoint] = constants.OEMPath
		}

		persistentDevice := u.spec.Partitions.Persistent
		if persistentDevice != nil && persistentDevice.MountPoint != "" {
			extraMounts[persistentDevice.MountPoint] = constants.UsrLocalPath
		}

		return ChrootHook(u.config, hook, u.spec.Active.MountPoint, extraMounts)
	}
	return Hook(u.config, hook)
}

func (u *UpgradeQCS6490Action) upgradeInstallStateYaml(meta interface{}, img v1.Image) error {
	if u.spec.Partitions.Recovery == nil || u.spec.Partitions.State == nil {
		return fmt.Errorf("undefined state or recovery partition")
	}

	if u.spec.State == nil {
		u.spec.State = &v1.InstallState{
			Partitions: map[string]*v1.PartitionState{},
		}
	}

	u.spec.State.Date = time.Now().Format(time.RFC3339)
	imgState := &v1.ImageState{
		Source:         img.Source,
		SourceMetadata: meta,
		Label:          img.Label,
		FS:             img.FS,
	}
	if u.spec.RecoveryUpgrade {
		recoveryPart := u.spec.State.Partitions[constants.RecoveryPartName]
		if recoveryPart == nil {
			recoveryPart = &v1.PartitionState{
				Images: map[string]*v1.ImageState{},
			}
			u.spec.State.Partitions[constants.RecoveryPartName] = recoveryPart
		}
		recoveryPart.Images[constants.RecoveryImgName] = imgState
	} else {
		statePart := u.spec.State.Partitions[constants.StatePartName]
		if statePart == nil {
			statePart = &v1.PartitionState{
				Images: map[string]*v1.ImageState{},
			}
			u.spec.State.Partitions[constants.StatePartName] = statePart
		}
		statePart.Images[constants.PassiveImgName] = statePart.Images[constants.ActiveImgName]
		statePart.Images[constants.ActiveImgName] = imgState
	}

	return u.config.WriteInstallState(
		u.spec.State,
		filepath.Join(u.spec.Partitions.State.MountPoint, constants.InstallStateFile),
		filepath.Join(u.spec.Partitions.Recovery.MountPoint, constants.InstallStateFile),
	)
}

func (u *UpgradeQCS6490Action) Run() (err error) {
	var systemPartition, passivePartition *v1.Partition
	e := elemental.NewElemental(u.config)

	_, err = u.config.Runner.Run("sgdisk", "-V")
	if err != nil {
		return err
	}
	_, err = u.config.Runner.Run("parted", "-v")
	if err != nil {
		return err
	}

	u.Info("Starting upgrade")
	u.Info("Source: %s", u.spec.Active.Source.Value())
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	part, _ := partitions.GetAllPartitions()
	for _, p := range part {
		switch p.Label {
		case "system":
			u.Debug("Found system partition: %s", litter.Sdump(p))
			systemPartition = p
		case "passive":
			u.Debug("Found passive partition: %s", litter.Sdump(p))
			passivePartition = p
		}
	}

	if systemPartition == nil || passivePartition == nil {
		return fmt.Errorf("could not find system or passive partition")
	}

	// Set this so the log properly refers to this partition properly
	passivePartition.FilesystemLabel = constants.QCS6490_passive_label
	// Set mountpoint to mount the passive under
	passivePartition.MountPoint = constants.TransitionDir

	// Format passive
	err = partitioner.FormatDevice(u.config.Runner, passivePartition.Path, constants.LinuxFs, constants.QCS6490_system_label)
	if err != nil {
		return err
	}

	// Mount passive
	umount, err := e.MountRWPartition(passivePartition)
	if err != nil {
		return err
	}
	cleanup.Push(umount)

	// Deploy image into it
	u.Info("deploying image %s to %s", u.spec.Active.Source.Value(), passivePartition.MountPoint)
	_, err = e.DumpSource(passivePartition.MountPoint, u.spec.Active.Source)
	if err != nil {
		u.Error("Failed deploying image to target '%s': %s", passivePartition.MountPoint, err)
		return err
	}

	_, _ = u.config.Runner.Run("sync")

	// This is to get the partition number itself as sgdisk needs the number directly not the device
	disk := partitioner.NewPartedCall(passivePartition.Disk, u.config.Runner)
	prnt, err := disk.Print()
	parts := disk.GetPartitions(prnt)
	for _, p := range parts {
		if p.PLabel == constants.QCS6490_passive_label {
			// Change passive partition label to system
			_, _ = u.config.Runner.Run("sgdisk", passivePartition.Disk, "-c", fmt.Sprintf("%d:%s", p.Number, constants.QCS6490_system_label))
			// Change passive filesystem label to system (not needed, but nice to have)
			_, _ = u.config.Runner.Run("tune2fs", "-L", constants.QCS6490_system_label, passivePartition.Path)
		}
		if p.PLabel == constants.QCS6490_system_label {
			// Change active partition label to passive
			_, _ = u.config.Runner.Run("sgdisk", systemPartition.Disk, "-c", fmt.Sprintf("%d:%s", p.Number, constants.QCS6490_passive_label))
			// Change system filesystem label to passive (not needed, but nice to have)
			_, _ = u.config.Runner.Run("tune2fs", "-L", constants.QCS6490_passive_label, systemPartition.Path)
		}

	}

	_, _ = u.config.Runner.Run("sync")

	u.Info("Upgrade completed")
	if !u.spec.RecoveryUpgrade {
		u.config.Logger.Warn("Remember that recovery is upgraded separately by passing the --recovery flag to the upgrade command!\n" +
			"See more info about this on https://kairos.io/docs/upgrade/")
	}

	// Do not reboot/poweroff on cleanup errors
	if cleanErr := cleanup.Cleanup(err); cleanErr != nil {
		u.config.Logger.Warn("failure during cleanup (ignoring), run with --debug to see more details")
		u.config.Logger.Debug(cleanErr.Error())
	}

	return nil
}

// remove attempts to remove the given path. Does nothing if it doesn't exist
func (u *UpgradeQCS6490Action) remove(path string) error {
	if exists, _ := fsutils.Exists(u.config.Fs, path); exists {
		u.Debug("[Cleanup] Removing %s", path)
		return u.config.Fs.RemoveAll(path)
	}
	return nil
}
