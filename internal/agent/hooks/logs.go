package hook

import (
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	kcrypt "github.com/kairos-io/kcrypt/pkg/lib"
	"path/filepath"
	"strings"
	"syscall"
)

// CopyLogs copies all current logs to the persistent partition.
// useful during install to keep the livecd logs
// best effort, no error handling
type CopyLogs struct{}

func (k CopyLogs) Run(c config.Config, _ v1.Spec) error {
	c.Logger.Logger.Debug().Msg("Running CopyLogs hook")
	_ = machine.Umount(constants.PersistentDir)

	// Config passed during install ends up here, kcrypt challenger needs to read it if we are using a server for encryption
	_ = machine.Mount(constants.OEMLabel, constants.OEMPath)
	defer func() {
		_ = machine.Umount(constants.OEMPath)
	}()

	// Path if we have encrypted persistent
	if len(c.Install.Encrypt) != 0 {
		err := kcrypt.UnlockAll(false)
		if err != nil {
			return err
		}
		// Close all the unencrypted partitions at the end!
		defer func() {
			for _, p := range c.Install.Encrypt {
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
	}

	_, _ = utils.SH("udevadm trigger --type=all || udevadm trigger")
	err := c.Syscall.Mount(filepath.Join("/dev/disk/by-label", constants.PersistentLabel), constants.PersistentDir, "ext4", 0, "")
	if err != nil {
		fmt.Printf("could not mount persistent: %s\n", err)
		return err
	}

	defer func() {
		err := machine.Umount(constants.PersistentDir)
		if err != nil {
			c.Logger.Errorf("could not unmount persistent partition: %s", err)
		}
	}()

	// Create the directory on persistent
	varLog := filepath.Join(constants.PersistentDir, ".state", "var-log.bind")

	err = fsutils.MkdirAll(c.Fs, varLog, 0755)
	if err != nil {
		c.Logger.Errorf("could not create directory on persistent partition: %s", err)
		return nil
	}
	// Copy all current logs to the persistent partition
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, "/var/log/", varLog, []string{}...)
	if err != nil {
		c.Logger.Errorf("could not copy logs to persistent partition: %s", err)
		return nil
	}
	syscall.Sync()
	c.Logger.Debugf("Logs copied to persistent partition")
	c.Logger.Logger.Debug().Msg("Finish CopyLogs hook")
	return nil
}
