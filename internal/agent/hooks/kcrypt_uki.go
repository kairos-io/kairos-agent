package hook

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	kcrypt "github.com/kairos-io/kcrypt/pkg/lib"
)

type KcryptUKI struct{}

func (k KcryptUKI) Run(c config.Config, spec v1.Spec) error {
	// pre-check for systemd version, we need something higher or equal to 252
	run, err := utils.SH("systemctl --version | head -1 | awk '{ print $2}'")
	systemdVersion := strings.TrimSpace(string(run))
	if err != nil {
		c.Logger.Errorf("could not get systemd version: %s", err)
		c.Logger.Errorf("could not get systemd version: %s", run)
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

	// Check for a TPM 2.0 device as its needed to encrypt
	// Exposed by the kernel to userspace as /dev/tpmrm0 since kernel 4.12
	// https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/commit/?id=fdc915f7f71939ad5a3dda3389b8d2d7a7c5ee66
	_, err = os.Stat("/dev/tpmrm0")
	if err != nil {
		c.Logger.Warnf("Skipping partition encryption, could not find TPM 2.0 device at /dev/tpmrm0")
		return nil
	}

	c.Logger.Logger.Debug().Msg("Running KcryptUKI hook")

	// We always encrypt OEM and PERSISTENT under UKI
	// If mounted, unmount it
	_ = machine.Umount(constants.OEMDir)        //nolint:errcheck
	_ = machine.Umount(constants.PersistentDir) //nolint:errcheck

	// Backup oem as we already copied files on there and on luksify it will be wiped
	err = machine.Mount(constants.OEMLabel, constants.OEMDir)
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

	for _, p := range append([]string{constants.OEMLabel, constants.PersistentLabel}, c.Install.Encrypt...) {
		c.Logger.Infof("Encrypting %s", p)
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

	_, _ = utils.SH("sync")

	err = kcrypt.UnlockAll(true)
	if err != nil {
		c.Logger.Errorf("could not unlock partitions: %s", err)
		return err
	}
	// Close the unlocked partitions after dealing with them, otherwise we leave them open and they can be mounted by anyone
	defer func() {
		for _, p := range append([]string{constants.OEMLabel, constants.PersistentLabel}, c.Install.Encrypt...) {
			c.Logger.Debugf("Closing unencrypted /dev/disk/by-label/%s", p)
			out, err := utils.SH(fmt.Sprintf("cryptsetup close /dev/disk/by-label/%s", p))
			// There is a known error with cryptsetup that it can't close the device because of a semaphore
			// doesnt seem to affect anything as the device is closed as expected so we ignore it if it matches the
			// output of the error
			if err != nil && !strings.Contains(out, "incorrect semaphore state") {
				c.Logger.Errorf("could not close /dev/disk/by-label/%s: %s", p, out)
			}
		}
	}()

	// Here it can take the oem partition a bit of time to appear after unlocking so we need to retry a couple of time with some waiting
	// retry + backoff
	// Check all encrypted partitions are unlocked
	for _, p := range append([]string{constants.OEMLabel, constants.PersistentLabel}) {
		for i := 0; i < 10; i++ {
			c.Logger.Infof("Waiting for unlocked partition %s to appear", p)
			_, _ = utils.SH("sync")
			part, _ := utils.SH(fmt.Sprintf("blkid -L %s", p))
			if part == "" {
				c.Logger.Infof("Partition %s not found, waiting %d seconds before retrying", p, i)
				time.Sleep(time.Duration(i) * time.Second)
				// Retry the unlock as well, because maybe the partition was not refreshed on time for unlock to unlock it
				// So no matter how many tries we do, it will still be locked and will never appear
				err := kcrypt.UnlockAll(true)
				if err != nil {
					c.Logger.Debugf("UnlockAll returned: %s", err)
				}
				if i == 9 {
					c.Logger.Errorf("Partition %s not unlocked/found after 10 retries", p)
					return fmt.Errorf("partition %s not unlocked/found after 10 retries", p)
				}
				continue
			}
			c.Logger.Infof("Partition found, continuing")
			break
		}
	}

	err = machine.Mount(constants.OEMLabel, constants.OEMDir)
	if err != nil {
		return err
	}
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, tmpDir, constants.OEMDir, []string{}...)
	if err != nil {
		return err
	}
	err = machine.Umount(constants.OEMDir)
	if err != nil {
		return err
	}

	c.Logger.Logger.Debug().Msg("Finish KcryptUKI hook")
	// We now have the partitions unlocked and ready, lets call the other hooks here instead of closing and reopening them each time
	err = BundlePostInstall{}.Run(c, spec)
	if err != nil {
		return err
	}
	_ = CustomMounts{}.Run(c, spec)
	err = CopyLogs{}.Run(c, spec)
	if err != nil {
		return err
	}
	return nil
}
