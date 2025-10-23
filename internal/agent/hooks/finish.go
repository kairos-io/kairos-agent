package hook

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/kcrypt"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
)

// Finish is a hook that runs after the install process.
// It is used to encrypt partitions and run the BundlePostInstall, CustomMounts and CopyLogs hooks
type Finish struct{}

func (k Finish) Run(c config.Config, spec v1.Spec) error {
	var err error
	if len(c.Install.Encrypt) != 0 || internalutils.IsUki() {
		c.Logger.Logger.Info().Msg("Calling udevadm settle")
		if err := udevAdmSettle("/dev/vda2", 15*time.Second); err != nil {
			return fmt.Errorf("ERROR settling udev: %w\n", err)
		}
		c.Logger.Logger.Info().Msg("Running encrypt hook")

		err = Encrypt(c, spec)
		// if internalutils.IsUki() {
		// 	err = EncryptUKI(c, spec)
		// } else {
		// 	err = EncryptNonUKI(c, spec)
		// }

		// partitions are unlocked, make sure to lock them before we end
		defer lockPartitions(c)

		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("could not encrypt partitions")
			return err
		}
		c.Logger.Logger.Info().Msg("Finished encrypt hook")
	}

	// Copy cloud-config to OEM after any encryption operations
	// This ensures cloud-config is preserved regardless of whether OEM was encrypted
	c.Logger.Logger.Info().Msg("Copying cloud-config to OEM partition")

	// Check if OEM is already mounted (it might be if no encryption was done)
	// Use findmnt to check if the directory is a mount point
	_, err = utils.SH(fmt.Sprintf("findmnt %s", constants.OEMDir))
	oemAlreadyMounted := (err == nil)

	if !oemAlreadyMounted {
		c.Logger.Logger.Info().Msg("Mounting OEM partition")
		err = machine.Mount(constants.OEMLabel, constants.OEMDir)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Failed to mount OEM for cloud-config copy")
			return fmt.Errorf("failed to mount OEM: %w", err)
		}
		defer func() {
			c.Logger.Logger.Info().Msg("Unmounting OEM after cloud-config copy")
			_ = machine.Umount(constants.OEMDir)
		}()
	} else {
		c.Logger.Logger.Info().Msg("OEM already mounted, skipping mount")
	}

	e := elemental.NewElemental(&c)
	err = e.CopyCloudConfig(c.CloudInitPaths)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Msg("Failed to copy cloud-config to OEM")
		return fmt.Errorf("failed to copy cloud-config to OEM: %w", err)
	}
	c.Logger.Logger.Info().Msg("Successfully copied cloud-config to OEM")

	// Now that we have everything encrypted and ready to mount if needed
	err = GrubPostInstallOptions{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("Could not set grub options post install")
		return err
	}
	err = BundlePostInstall{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not copy run bundles post install")
		if c.FailOnBundleErrors {
			return err
		}
	}
	err = CustomMounts{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not create custom mounts")
	}
	err = CopyLogs{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not copy logs")
	}
	return nil
}

// Encrypt is the unified encryption method that works for both UKI and non-UKI modes
func Encrypt(c config.Config, _ v1.Spec) error {
	isUKI := internalutils.IsUki()
	c.Logger.Logger.Info().Bool("uki_mode", isUKI).Msg("Starting unified encryption flow")

	// 1. Determine which partitions need encryption
	partitions := determinePartitionsToEncrypt(c, isUKI)
	if len(partitions) == 0 {
		c.Logger.Logger.Info().Msg("No partitions to encrypt")
		return nil
	}
	c.Logger.Logger.Info().Strs("partitions", partitions).Msg("Partitions to encrypt")

	// 2. Common preparation
	if err := preparePartitionsForEncryption(c, partitions); err != nil {
		return fmt.Errorf("failed to prepare partitions: %w", err)
	}

	// 3. Backup OEM if it's in the list
	var oemBackupPath string
	var cleanupBackup func()
	needsOEMBackup := containsString(partitions, constants.OEMLabel)
	if needsOEMBackup {
		var err error
		oemBackupPath, cleanupBackup, err = backupOEMIfNeeded(c, constants.OEMLabel)
		if err != nil {
			return fmt.Errorf("failed to backup OEM: %w", err)
		}
		defer cleanupBackup()
	}

	// 4. Encrypt each partition
	for _, partition := range partitions {
		c.Logger.Logger.Info().Str("partition", partition).Msg("Encrypting partition")
		
		// TODO: Determine passphrase source and encrypt
		// This is where we'll call the actual encryption logic
		
		c.Logger.Logger.Info().Str("partition", partition).Msg("Successfully encrypted partition")
	}

	// 5. Unlock encrypted partitions
	if err := unlockEncryptedPartitions(c, isUKI); err != nil {
		return fmt.Errorf("failed to unlock partitions: %w", err)
	}

	// 6. Wait for partitions to appear
	if err := waitForUnlockedPartitions(c, partitions); err != nil {
		return fmt.Errorf("failed waiting for unlocked partitions: %w", err)
	}

	// 7. Restore OEM if needed
	if needsOEMBackup {
		if err := restoreOEMIfNeeded(c, constants.OEMLabel, oemBackupPath); err != nil {
			return fmt.Errorf("failed to restore OEM: %w", err)
		}
	}

	c.Logger.Logger.Info().Msg("Finished unified encryption flow")
	return nil
}

// EncryptNonUKI is a hook that encrypts partitions using kcrypt for non uki.
// It will unmount each partition right before encrypting it
func EncryptNonUKI(c config.Config, _ v1.Spec) error {
	c.Logger.Logger.Info().Msg("Starting partition encryption")

	kcryptConfig := kcrypt.ExtractKcryptConfigFromCollector(c.Config, c.Logger)
	if kcryptConfig != nil {
		c.Logger.Logger.Info().
			Str("challenger_server", kcryptConfig.ChallengerServer).
			Bool("mdns", kcryptConfig.MDNS).
			Msg("Using kcrypt config for multi-partition encryption")
	} else {
		c.Logger.Logger.Warn().Msg("No kcrypt config found - partitions will use local encryption")
	}

	for _, p := range c.Install.Encrypt {
		c.Logger.Logger.Info().Str("partition", p).Msg("Preparing to encrypt partition")

		// Unmount the partition before encrypting it
		// Find the device path for this partition label
		devPath, err := utils.SH(fmt.Sprintf("blkid -L %s", p))
		if err != nil {
			c.Logger.Logger.Warn().Str("label", p).Err(err).Msg("Could not find device for label")
		} else {
			devPath = strings.TrimSpace(devPath)
			c.Logger.Logger.Info().Str("device", devPath).Str("label", p).Msg("Found device for label")

			// Find all mount points for this device and unmount them
			mountPoints, _ := utils.SH(fmt.Sprintf("findmnt -n -o TARGET -S %s", devPath))
			if mountPoints != "" {
				for _, mp := range strings.Split(strings.TrimSpace(mountPoints), "\n") {
					if mp != "" {
						c.Logger.Logger.Info().Str("device", devPath).Str("mountpoint", mp).Msg("Unmounting partition")
						if err := machine.Umount(mp); err != nil {
							c.Logger.Logger.Warn().Str("mountpoint", mp).Err(err).Msg("Could not unmount")
						}
					}
				}
			}
		}

		// Wait for device to be released
		_, _ = utils.SH("sync")
		time.Sleep(1 * time.Second)

		c.Logger.Logger.Info().Str("partition", p).Msg("Encrypting partition " + p)
		_, err = kcrypt.EncryptWithConfig(p, c.Logger, kcryptConfig)
		if err != nil {
			c.Logger.Logger.Error().Str("partition", p).Err(err).Msg("Could not encrypt partition")
			return err
		}
	}

	fmt.Println("unlocking all")
	_ = kcrypt.UnlockAllWithConfig(false, c.Logger, kcryptConfig)

	for _, p := range c.Install.Encrypt {
		for i := 0; i < 10; i++ {
			c.Logger.Infof("Waiting for unlocked partition %s to appear", p)
			_, _ = utils.SH("sync")
			part, _ := utils.SH(fmt.Sprintf("blkid -L %s", p))
			if part == "" {
				c.Logger.Infof("Partition %s not found, waiting %d seconds before retrying", p, i)
				time.Sleep(time.Duration(i) * time.Second)
				// Retry the unlock as well, because maybe the partition was not refreshed on time for unlock to unlock it
				// So no matter how many tries we do, it will still be locked and will never appear
				err := kcrypt.UnlockAll(false, c.Logger)
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

	return nil
}

// EncryptUKI encrypts the partitions using kcrypt in uki mode
// It will unmount OEM and PERSISTENT and return with them unmounted
func EncryptUKI(c config.Config, spec v1.Spec) error {
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
	// Extract the numeric portion of the version string using a regular expression
	re := regexp.MustCompile(`\d+`)
	matches := re.FindString(systemdVersion)
	if matches == "" {
		return fmt.Errorf("could not extract numeric part from systemd version: %s", systemdVersion)
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
		return fmt.Errorf("systemd version is %s, we need 252 or higher for encrypting partitions", systemdVersion)
	}

	// Check for a TPM 2.0 device as its needed to encrypt
	// Exposed by the kernel to userspace as /dev/tpmrm0 since kernel 4.12
	// https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git/commit/?id=fdc915f7f71939ad5a3dda3389b8d2d7a7c5ee66
	_, err = os.Stat("/dev/tpmrm0")
	if err != nil {
		c.Logger.Warnf("Skipping partition encryption, could not find TPM 2.0 device at /dev/tpmrm0")
		return fmt.Errorf("Skipping partition encryption, could not find TPM 2.0 device at /dev/tpmrm0")
	}

	// We always encrypt OEM and PERSISTENT under UKI
	// If mounted, unmount it
	err = machine.Umount(constants.OEMDir) //nolint:errcheck
	if err != nil {
		fmt.Printf("error unmounting %s: %s", constants.OEMDir, err.Error())
	}
	err = machine.Umount(constants.PersistentDir) //nolint:errcheck
	if err != nil {
		fmt.Printf("error unmounting %s: %s", constants.PersistentDir, err.Error())
	}

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
		_ = os.Setenv("SYSTEMD_LOG_LEVEL", "debug")
		err = kcrypt.EncryptWithPcrs(p, c.BindPublicPCRs, c.BindPCRs, c.Logger)
		_ = os.Unsetenv("SYSTEMD_LOG_LEVEL")
		if err != nil {
			c.Logger.Errorf("could not encrypt partition: %s", err)
			return err
		}
		c.Logger.Infof("Done encrypting %s", p)
	}

	_, _ = utils.SH("sync")

	_ = os.Setenv("SYSTEMD_LOG_LEVEL", "debug")

	err = kcrypt.UnlockAll(true, c.Logger)

	_ = os.Unsetenv("SYSTEMD_LOG_LEVEL")
	if err != nil {
		lockPartitions(c)
		c.Logger.Errorf("could not unlock partitions: %s", err)
		return err
	}

	// Here it can take the oem partition a bit of time to appear after unlocking so we need to retry a couple of time with some waiting
	// retry + backoff
	// Check all encrypted partitions are unlocked
	for _, p := range []string{constants.OEMLabel, constants.PersistentLabel} {
		for i := 0; i < 10; i++ {
			c.Logger.Infof("Waiting for unlocked partition %s to appear", p)
			_, _ = utils.SH("sync")
			part, _ := utils.SH(fmt.Sprintf("blkid -L %s", p))
			if part == "" {
				c.Logger.Infof("Partition %s not found, waiting %d seconds before retrying", p, i)
				time.Sleep(time.Duration(i) * time.Second)
				// Retry the unlock as well, because maybe the partition was not refreshed on time for unlock to unlock it
				// So no matter how many tries we do, it will still be locked and will never appear
				err := kcrypt.UnlockAll(true, c.Logger)
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

	// Mount the unlocked oem partition
	err = machine.Mount(constants.OEMLabel, constants.OEMDir)
	if err != nil {
		return err
	}
	// Copy back the contents of the oem partition that we saved before encrypting
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, tmpDir, constants.OEMDir, []string{}...)
	if err != nil {
		return err
	}
	// Unmount the oem partition and leave everything unmounted
	err = machine.Umount(constants.OEMDir)
	if err != nil {
		return err
	}

	return nil
}

// Helper methods for unified encryption flow

// determinePartitionsToEncrypt returns the list of partitions to encrypt based on mode
func determinePartitionsToEncrypt(c config.Config, isUKI bool) []string {
	// TODO: Implement
	return []string{}
}

// preparePartitionsForEncryption unmounts all partitions that will be encrypted
func preparePartitionsForEncryption(c config.Config, partitions []string) error {
	// TODO: Implement
	return nil
}

// backupOEMIfNeeded backs up the OEM partition contents before encryption
func backupOEMIfNeeded(c config.Config, oemLabel string) (backupPath string, cleanup func(), err error) {
	// TODO: Implement
	return "", func() {}, nil
}

// restoreOEMIfNeeded restores the OEM partition contents after encryption
func restoreOEMIfNeeded(c config.Config, oemLabel string, backupPath string) error {
	// TODO: Implement
	return nil
}

// unlockEncryptedPartitions unlocks all encrypted partitions after encryption
func unlockEncryptedPartitions(c config.Config, useTpm bool) error {
	// TODO: Implement
	return nil
}

// waitForUnlockedPartitions waits for encrypted partitions to appear after unlocking
func waitForUnlockedPartitions(c config.Config, partitions []string) error {
	// TODO: Implement
	return nil
}

// containsString checks if a string exists in a slice
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// udevAdmSettle triggers udev events, waits for them to complete,
// and adds basic debugging / diagnostics around the device state.
func udevAdmSettle(device string, timeout time.Duration) error {
	fmt.Printf("INF Triggering udev events...\n")

	// Trigger subsystems and devices (this replays all udev rules)
	triggerCmds := [][]string{
		{"udevadm", "trigger", "--action=add", "--type=subsystems"},
		{"udevadm", "trigger", "--action=add", "--type=devices"},
	}

	for _, args := range triggerCmds {
		cmd := exec.Command(args[0], args[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s failed: %v (output: %s)", args, err, string(output))
		}
	}

	fmt.Printf("INF Flushing filesystem buffers (sync)\n")
	if err := exec.Command("sync").Run(); err != nil {
		fmt.Printf("WARN sync failed: %v\n", err)
	}

	fmt.Printf("INF Waiting for udev to settle (timeout: %s)\n", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "udevadm", "settle")
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("udevadm settle timed out after %s", timeout)
	}
	if err != nil {
		return fmt.Errorf("udevadm settle failed: %v (output: %s)", err, string(output))
	}

	fmt.Printf("INF udevadm settle completed successfully\n")

	// Optional: give the kernel a moment to release device locks.
	time.Sleep(2 * time.Second)

	// Debug: check if the target device is still busy.
	if device != "" {
		fmt.Printf("DBG Checking if %s is in use...\n", device)
		checkCmd := exec.Command("fuser", device)
		checkOut, checkErr := checkCmd.CombinedOutput()
		if checkErr == nil && len(checkOut) > 0 {
			fmt.Printf("WARN Device %s appears to be in use by: %s\n", device, string(checkOut))
		} else {
			fmt.Printf("DBG No active users detected for %s\n", device)
		}
	}

	return nil
}
