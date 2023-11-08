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

	cloudConfig := `name: "Default user, permissions and serial login"
stages:
  post-upgrade-qcs6490:
    - name: "Setup groups"
      ensure_entities:
        - entity: |
            kind: "group"
            group_name: "admin"
            password: "x"
            gid: 900
    - name: "Setup users"
      users:
        kairos:
          passwd: "!"
          shell: /bin/bash
          homedir: "/home/kairos"
          groups:
            - "admin"
    - name: "Set user password"
      users:
        kairos:
          passwd: "kairos"
    - name: "Setup sudo"
      files:
        - path: "/etc/sudoers"
          owner: 0
          group: 0
          permsisions: 0600
          content: |
            Defaults always_set_home
            Defaults secure_path="/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/bin:/usr/local/sbin"
            Defaults env_reset
            Defaults env_keep = "LANG LC_ADDRESS LC_CTYPE LC_COLLATE LC_IDENTIFICATION LC_MEASUREMENT LC_MESSAGES LC_MONETARY LC_NAME LC_NUMERIC LC_PAPER LC_TELEPHONE LC_ATIME LC_ALL LANGUAGE LINGUAS XDG_SESSION_COOKIE"
            Defaults !insults
            root ALL=(ALL) ALL
            %admin ALL=(ALL) NOPASSWD: ALL
            #includedir /etc/sudoers.d
      commands:
        - passwd -l root
    - name: "Create systemd services"
      files:
        - path: /etc/systemd/system/cos-setup-boot.service
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Unit]
            Description=cOS system configuration
            Before=getty.target
            
            [Service]
            Type=oneshot
            RemainAfterExit=yes
            ExecStart=/usr/bin/kairos-agent run-stage boot
            
            [Install]
            WantedBy=multi-user.target
        - path: /etc/systemd/system/cos-setup-fs.service
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Unit]
            Description=cOS system after FS setup
            DefaultDependencies=no
            After=local-fs.target
            Wants=local-fs.target
            Before=sysinit.target
            
            [Service]
            Type=oneshot
            RemainAfterExit=yes
            ExecStart=/usr/bin/kairos-agent run-stage fs
            
            [Install]
            WantedBy=sysinit.target
        - path: /etc/systemd/system/cos-setup-network.service
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Unit]
            Description=cOS setup after network
            After=network-online.target
            
            [Service]
            Type=oneshot
            RemainAfterExit=yes
            ExecStart=/usr/bin/kairos-agent run-stage network
            
            [Install]
            WantedBy=multi-user.target
        - path: /etc/systemd/system/cos-setup-reconcile.service
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Unit]
            Description=cOS setup reconciler
            
            [Service]
            Nice=19
            IOSchedulingClass=2
            IOSchedulingPriority=7
            Type=oneshot
            ExecStart=/bin/bash -c "systemd-inhibit /usr/bin/kairos-agent run-stage reconcile"
            TimeoutStopSec=180
            KillMode=process
            KillSignal=SIGINT
            
            [Install]
            WantedBy=multi-user.target
        - path: /etc/systemd/system/cos-setup-reconcile.timer
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Unit]
            Description=cOS setup reconciler
            
            [Timer]
            OnBootSec=5min
            OnUnitActiveSec=60min
            Unit=cos-setup-reconcile.service
            
            [Install]
            WantedBy=multi-user.target
    - name: "Enable systemd services"
      commands:
        - ln -sf /etc/systemd/system/cos-setup-reconcile.timer /etc/systemd/system/multi-user.target.wants/cos-setup-reconcile.timer
        - ln -sf /etc/systemd/system/cos-setup-fs.service /etc/systemd/system/sysinit.target.wants/cos-setup-fs.service
        - ln -sf /etc/systemd/system/cos-setup-boot.service /etc/systemd/system/multi-user.target.wants/cos-setup-boot.service
        - ln -sf /etc/systemd/system/cos-setup-network.service /etc/systemd/system/multi-user.target.wants/cos-setup-network.service
    - name: "Enable systemd-network config files for DHCP"
      if: '[ -e "/sbin/systemctl" ] || [ -e "/usr/bin/systemctl" ] || [ -e "/usr/sbin/systemctl" ] || [ -e "/usr/bin/systemctl" ]'
      files:
        - path: /etc/systemd/network/20-dhcp.network
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Match]
            Name=en*
            [Network]
            DHCP=yes
        - path: /etc/systemd/network/20-dhcp-legacy.network
          permissions: 0644
          owner: 0
          group: 0
          content: |
            [Match]
            Name=eth*
            [Network]
            DHCP=yes
    - name: "Disable NetworkManager and wicked"
      systemctl:
        disable:
          - NetworkManager
          - wicked
    - name: "Enable systemd-network and systemd-resolved"
      systemctl:
        enable:
          - systemd-networkd
          - systemd-resolved
    - name: "Link /etc/resolv.conf to systemd resolv.conf"
      commands:
        - rm /etc/resolv.conf
        - ln -s /run/systemd/resolve/resolv.conf /etc/resolv.conf
`

	fsutils.MkdirAll(u.config.Fs, filepath.Join(passivePartition.MountPoint, "run", "cos"), 0755)
	fsutils.MkdirAll(u.config.Fs, filepath.Join(passivePartition.MountPoint, "oem"), 0755)
	u.config.Fs.WriteFile(filepath.Join(passivePartition.MountPoint, "run", "cos", "live_mode"), []byte(""), 0755)
	u.config.Fs.WriteFile(filepath.Join(passivePartition.MountPoint, "oem", "34_user.yaml"), []byte(cloudConfig), 0755)

	err = ChrootHook(u.config, "post-upgrade-qcs6490", passivePartition.MountPoint, nil)
	if err != nil {
		return err
	}

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
