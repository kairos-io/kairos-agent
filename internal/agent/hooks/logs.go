package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/machine"
	"path/filepath"
	"syscall"
)

// CopyLogs copies all current logs to the persistent partition
// useful during install to keep the livecd logs
// best effort, no error handling
type CopyLogs struct{}

func (k CopyLogs) Run(c config.Config, _ v1.Spec) error {
	c.Logger.Debugf("Copying logs to persistent partition")
	_ = machine.Umount(constants.PersistentDir)

	err := machine.Mount(constants.PersistentLabel, constants.PersistentDir)
	if err != nil {
		c.Logger.Errorf("could not mount persistent partition: %s", err)
		return nil
	}

	// Create the directory on persistent if it doesn't exist
	exists, _ := fsutils.Exists(c.Fs, filepath.Join(constants.PersistentDir, ".state", "var-log.bind"))
	if !exists {
		_ = fsutils.MkdirAll(c.Fs, filepath.Join(constants.PersistentDir, ".state", "var-log.bind"), 0755)
	}
	// Copy all current logs to the persistent partition
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, "/var/log/", filepath.Join(constants.PersistentDir, ".state", "var-log.bind"), []string{}...)
	if err != nil {
		c.Logger.Errorf("could not copy logs to persistent partition: %s", err)
		return nil
	}
	err = machine.Umount(constants.PersistentDir)
	if err != nil {
		c.Logger.Errorf("could not unmount persistent partition: %s", err)
		return nil
	}
	syscall.Sync()
	c.Logger.Debugf("Logs copied to persistent partition")
	return nil
}
