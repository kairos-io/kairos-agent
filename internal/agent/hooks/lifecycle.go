package hook

import (
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
)

type Lifecycle struct{}

func (s Lifecycle) Run(c config.Config, spec v1.Spec) error {
	c.Logger.Logger.Debug().Msg("Running Lifecycle hook")
	if spec.ShouldReboot() {
		err := utils.RebootWithConfig(c.Runner, 5*time.Second, c.Install)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Failed to reboot system")
			return err
		}
	}

	if spec.ShouldShutdown() {
		err := utils.ShutdownWithConfig(c.Runner, 5*time.Second, c.Install)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Failed to shutdown system")
			return err
		}
	}
	c.Logger.Logger.Debug().Msg("Finish Lifecycle hook")
	return nil
}
