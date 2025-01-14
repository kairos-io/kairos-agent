package hook

import (
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/system"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config, _ v1.Spec) error {
	if len(c.Install.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", c.Install.GrubOptions)
	err := system.Apply(system.SetGRUBOptions(c.Install.GrubOptions))
	if err != nil && !strings.Contains(err.Error(), "0 errors occurred") {
		c.Logger.Logger.Error().Err(err).Msg("Failed to set grub options")
	}
	c.Logger.Logger.Debug().Msg("Finish GrubOptions hook")
	return nil
}

type GrubPostInstallOptions struct{}

func (b GrubPostInstallOptions) Run(c config.Config, _ v1.Spec) error {
	if len(c.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running GrubOptions hook")
	err := system.Apply(system.SetGRUBOptions(c.GrubOptions))
	if err != nil {
		c.Logger.Logger.Error().Err(err).Msg("Failed to set grub options")
	}
	c.Logger.Logger.Debug().Msg("Running GrubOptions hook")
	return nil
}
