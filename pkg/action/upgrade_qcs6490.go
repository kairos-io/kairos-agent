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
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	"github.com/kairos-io/kairos-agent/v2/pkg/partitioner"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	boardutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/boards"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/sanity-io/litter"
)

// UpgradeQCS6490Action represents the struct that will run the upgrade from start to finish for a QCS6490 board
type UpgradeQCS6490Action struct {
	config *agentConfig.Config
	spec   *v1.UpgradeSpec
}

func NewUpgradeQCS6490Action(config *agentConfig.Config, spec *v1.UpgradeSpec) *UpgradeQCS6490Action {
	return &UpgradeQCS6490Action{config: config, spec: spec}
}

func (u *UpgradeQCS6490Action) Run() (err error) {
	var systemPartition, passivePartition *v1.Partition
	e := elemental.NewElemental(u.config)

	err = boardutils.CheckDeps(u.config.Runner)
	if err != nil {
		u.config.Logger.Errorf("Failed checking dependencies: %s", err)
		return err
	}

	u.config.Logger.Infof("Starting upgrade")
	u.config.Logger.Infof("Source: %s", u.spec.Active.Source.Value())
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()

	err, systemPartition, passivePartition = boardutils.GetPartitions()
	if err != nil {
		return err
	}
	u.config.Logger.Debugf("Found system partition: %s", litter.Sdump(systemPartition))
	u.config.Logger.Debugf("Found passive partition: %s", litter.Sdump(passivePartition))

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
	u.config.Logger.Infof("deploying image %s to %s", u.spec.Active.Source.Value(), passivePartition.MountPoint)
	_, err = e.DumpSource(passivePartition.MountPoint, u.spec.Active.Source)
	if err != nil {
		u.config.Logger.Errorf("Failed deploying image to target '%s': %s", passivePartition.MountPoint, err)
		return err
	}

	_, _ = u.config.Runner.Run("sync")

	err, out := boardutils.SetPassiveActive(u.config.Runner)
	if err != nil {
		u.config.Logger.Errorf("Failed setting passive partition as active: %s", err)
		u.config.Logger.Errorf("Failed setting passive partition as active(command output): %s", out)
		return err
	}

	u.config.Logger.Infof("Upgrade completed")
	if !u.spec.RecoveryUpgrade {
		u.config.Logger.Warn("Remember that recovery is upgraded separately by passing the --recovery flag to the upgrade command!\n" +
			"See more info about this on https://kairos.io/docs/upgrade/")
	}

	// Do not reboot/poweroff on cleanup errors
	if cleanErr := cleanup.Cleanup(err); cleanErr != nil {
		u.config.Logger.Warn("failure during cleanup (ignoring), run with --debug to see more details")
		u.config.Logger.Debugf(cleanErr.Error())
	}

	return nil
}
