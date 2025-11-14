package hook

import (
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	"github.com/kairos-io/kairos-sdk/utils"
)

type Lifecycle struct{}

func (s Lifecycle) Run(c sdkConfig.Config, spec sdkSpec.Spec) error {
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
