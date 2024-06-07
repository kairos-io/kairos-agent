package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"io/fs"
	"path/filepath"
	"strings"
)

type SysExtPostInstall struct{}

func (b SysExtPostInstall) Run(c config.Config, _ v1.Spec) error {
	c.Logger.Logger.Debug().Msg("Running SysExtPostInstall hook")
	// mount efi partition
	efiPart, err := partitions.GetEfiPartition()
	if err != nil {
		c.Logger.Errorf("failed to get EFI partition: %s", err)
		if c.FailOnBundleErrors {
			return err
		}
		return nil
	}
	mounted, _ := c.Mounter.IsMountPoint(constants.EfiDir)

	if !mounted {
		err = c.Mounter.Mount(efiPart.Path, constants.EfiDir, efiPart.FS, []string{"rw"})
		if err != nil {
			c.Logger.Errorf("failed to mount EFI partition: %s", err)
			if c.FailOnBundleErrors {
				return err
			}
			return nil
		}
		defer func() {
			_ = c.Mounter.Unmount(constants.EfiDir)
		}()
	} else {
		// If its mounted, try to remount it RW
		err = c.Mounter.Mount(efiPart.Path, constants.EfiDir, efiPart.FS, []string{"remount,rw"})
		defer func() {
			_ = c.Mounter.Unmount(constants.EfiDir)
		}()
	}

	activeDir := filepath.Join(constants.EfiDir, "EFI/kairos/active.efi.extra.d/")
	passiveDir := filepath.Join(constants.EfiDir, "EFI/kairos/passive.efi.extra.d/")
	for _, dir := range []string{activeDir, passiveDir} {
		err = fsutils.MkdirAll(c.Fs, dir, 0755)
		if err != nil {
			c.Logger.Errorf("failed to create directory %s: %s", dir, err)
			if c.FailOnBundleErrors {
				return err
			}
			return nil
		}
	}

	err = fsutils.WalkDirFs(c.Fs, constants.LiveDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".sysext.raw") {
			// copy it to /EFI/Kairos/{active,passive}.efi.extra.d/
			err = fsutils.Copy(c.Fs, path, filepath.Join(activeDir, info.Name()))
			if err != nil {
				c.Logger.Errorf("failed to copy %s to %s: %s", path, activeDir, err)
				if c.FailOnBundleErrors {
					return err
				}
				return nil
			}
			c.Logger.Debugf("copied %s to %s", path, activeDir)
		}
		return nil
	})
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Done SysExtPostInstall hook")
	return nil
}
