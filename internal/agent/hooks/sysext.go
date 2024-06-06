package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/machine"
	"io/fs"
	"path/filepath"
	"strings"
)

type SysExtPostInstall struct{}

func (b SysExtPostInstall) Run(c config.Config, s v1.Spec) error {
	c.Logger.Logger.Debug().Msg("Running SysExtPostInstall hook")

	// mount efi partition
	mounted, _ := utils.IsMounted(&c, &v1.Partition{MountPoint: constants.EfiDir})
	if !mounted {
		machine.Mount(constants.EfiLabel, constants.EfiDir) //nolint:errcheck
		defer func() {
			machine.Umount(constants.EfiDir) //nolint:errcheck
		}()
	} else {
		machine.Remount("rw", constants.EfiDir) //nolint:errcheck
		defer func() {
			machine.Remount("ro", constants.EfiDir) //nolint:errcheck
		}()
	}

	activeDir := filepath.Join(constants.EfiDir, "EFI/kairos/active.efi.extra.d/")
	passiveDir := filepath.Join(constants.EfiDir, "EFI/kairos/passive.efi.extra.d/")
	err := fsutils.MkdirAll(c.Fs, activeDir, 0755)
	if err != nil {
		c.Logger.Errorf("failed to create directory %s: %s", activeDir, err)
		if c.FailOnBundleErrors {
			return err
		}
		return nil
	}
	err = fsutils.MkdirAll(c.Fs, passiveDir, 0755)
	if err != nil {
		c.Logger.Errorf("failed to create directory %s: %s", activeDir, err)
		if c.FailOnBundleErrors {
			return err
		}
		return nil
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
		}
		return nil
	})
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	return nil
}
