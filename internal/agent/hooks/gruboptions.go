package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/state"
	"path/filepath"
)

// GrubOptionsPostInstall is a hook that runs after the install process to add grub options.
type GrubOptionsPostInstall struct{}

func (b GrubOptionsPostInstall) Run(c config.Config, _ v1.Spec) error {
	if len(c.Install.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", c.Install.GrubOptions)
	err := grubOptions(c, c.Install.GrubOptions)
	if err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish GrubOptions hook")
	return nil
}

// GrubFirstBootOptions is a hook that runs on the first boot to add grub options.
type GrubFirstBootOptions struct{}

func (b GrubFirstBootOptions) Run(c config.Config, _ v1.Spec) error {
	if len(c.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", c.GrubOptions)
	err := grubOptions(c, c.GrubOptions)
	if err != nil {
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish GrubOptions hook")
	return nil
}

// grubOptions sets the grub options in the grubenv file
// It mounts the OEM partition if not already mounted
// If its mounted but RO, it remounts it as RW
func grubOptions(c config.Config, opts map[string]string) error {
	runtime, err := state.NewRuntime()
	if err != nil {
		return err
	}
	if !runtime.OEM.Mounted {
		err = machine.Mount(cnst.OEMLabel, cnst.OEMPath)
		defer func() {
			_ = machine.Umount(cnst.OEMPath)
		}()
	}
	err = utils.SetPersistentVariables(filepath.Join(runtime.OEM.MountPoint, "grubenv"), opts, &c)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Msg("Failed to set grub options")
	}
	return err
}
