package hook

import (
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/utils"
)

type Lifecycle struct{}

func (s Lifecycle) Run(c config.Config, spec v1.Spec) error {
	c.Logger.Logger.Debug().Msg("Running Lifecycle hook")
	if spec.ShouldReboot() {
		time.Sleep(5 * time.Second)
		utils.Reboot()
	}

	if spec.ShouldShutdown() {
		time.Sleep(5 * time.Second)
		utils.PowerOFF()
	}
	c.Logger.Logger.Debug().Msg("Finish Lifecycle hook")
	return nil
}
