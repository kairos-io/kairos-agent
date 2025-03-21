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
	"path/filepath"
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
