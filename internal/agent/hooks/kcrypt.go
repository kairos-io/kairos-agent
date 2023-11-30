package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/machine"
	kcrypt "github.com/kairos-io/kcrypt/pkg/lib"
)

type Kcrypt struct{}

func (k Kcrypt) Run(c config.Config, _ v1.Spec) error {

	if len(c.Install.Encrypt) == 0 {
		return nil
	}

	// Config passed during install ends up here, so we need to read it
	_ = machine.Mount("COS_OEM", "/oem")
	defer func() {
		_ = machine.Umount("/oem") //nolint:errcheck
	}()

	for _, p := range c.Install.Encrypt {
		_, err := kcrypt.Luksify(p, "luks1", false)
		if err != nil {
			c.Logger.Errorf("could not encrypt partition: %s", err)
			if c.FailOnBundleErrors {
				return err
			}
		}
	}

	return nil
}
