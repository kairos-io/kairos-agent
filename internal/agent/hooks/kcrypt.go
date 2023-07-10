package hook

import (
	"fmt"
	"github.com/kairos-io/kairos-sdk/machine"
	"time"

	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
)

type Kcrypt struct{}

func (k Kcrypt) Run(c config.Config) error {

	if len(c.Install.Encrypt) == 0 {
		return nil
	}

	// Config passed during install ends up here, so we need to read it
	_ = machine.Mount("COS_OEM", "/oem")
	defer func() {
		_ = machine.Umount("/oem") //nolint:errcheck
	}()

	for _, p := range c.Install.Encrypt {
		out, err := utils.SH(fmt.Sprintf("kcrypt encrypt %s", p))
		if err != nil {
			fmt.Printf("could not encrypt partition: %s\n", out+err.Error())
			if c.FailOnBundleErrors {
				return err
			}
			// Give time to show the error
			time.Sleep(10 * time.Second)
			return nil // do not error out
		}
	}

	return nil
}
