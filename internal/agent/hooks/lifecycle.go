package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/utils"
	"time"
)

type Lifecycle struct{}

func (s Lifecycle) Run(c config.Config, spec v1.Spec) error {
	if spec.ShouldReboot() || c.Install.Reboot {
		time.Sleep(5)
		utils.Reboot()
	}

	if spec.ShouldShutdown() || c.Install.Poweroff {
		time.Sleep(5)
		utils.PowerOFF()
	}
	return nil
}
