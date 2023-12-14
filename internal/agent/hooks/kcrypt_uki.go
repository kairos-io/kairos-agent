package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	kcrypt "github.com/kairos-io/kcrypt/pkg/lib"
	"strconv"
	"strings"
	"time"
)

type KcryptUKI struct{}

func (k KcryptUKI) Run(c config.Config, _ v1.Spec) error {
	// pre-check for systemd version, we need something higher or equal to 252
	run, err := c.Runner.Run("systemctl --version | head -1 | awk '{ print $2}'")
	systemdVersion := strings.TrimSpace(string(run))
	if err != nil {
		c.Logger.Errorf("could not get systemd version: %s", err)
		return err
	}
	if systemdVersion == "" {
		c.Logger.Errorf("could not get systemd version: %s", err)
		return err
	}
	// Change systemdVersion to int value
	systemdVersionInt, err := strconv.Atoi(systemdVersion)
	if err != nil {
		c.Logger.Errorf("could not convert systemd version to int: %s", err)
		return err
	}
	// If systemd version is less than 252 return
	if systemdVersionInt < 252 {
		c.Logger.Infof("systemd version is %s, we need 252 or higher for encrypting partitions", systemdVersion)
		return nil
	}

	// We always encrypt OEM and PERSISTENT under UKI
	// If mounted, unmount it
	_ = machine.Umount(constants.OEMDir)        //nolint:errcheck
	_ = machine.Umount(constants.PersistentDir) //nolint:errcheck

	// Backup oem as we already copied files on there and on luksify it will be wiped
	err = machine.Mount("COS_OEM", constants.OEMDir)
	if err != nil {
		return err
	}
	tmpDir, err := fsutils.TempDir(c.Fs, "", "oem-backup-xxxx")
	if err != nil {
		return err
	}

	// Remove everything when we finish
	defer c.Fs.RemoveAll(tmpDir) //nolint:errcheck

	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, constants.OEMDir, tmpDir, []string{}...)
	if err != nil {
		return err
	}
	err = machine.Umount(constants.OEMDir) //nolint:errcheck
	if err != nil {
		return err
	}

	for _, p := range []string{"COS_OEM", "COS_PERSISTENT"} {
		c.Logger.Infof("Encrypting %s", p)
		utils.SH("udevadm settle") //nolint:errcheck
		utils.SH("sync")           //nolint:errcheck
		_, err := kcrypt.Luksify(p, "luks2", true)
		if err != nil {
			c.Logger.Errorf("could not encrypt partition: %s", err)
			if c.FailOnBundleErrors {
				return err
			}
			// Give time to show the error
			time.Sleep(10 * time.Second)
			return nil // do not error out
		}
		c.Logger.Infof("Done encrypting %s", p)
	}

	// Restore OEM
	err = kcrypt.UnlockAll(true)
	if err != nil {
		return err
	}
	err = machine.Mount("COS_OEM", constants.OEMDir)
	if err != nil {
		return err
	}
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, tmpDir, constants.OEMDir, []string{}...)
	if err != nil {
		return err
	}
	err = machine.Umount(constants.OEMDir) //nolint:errcheck
	if err != nil {
		return err
	}
	return nil
}
