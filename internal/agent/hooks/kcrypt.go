package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/machine"
	kcrypt "github.com/kairos-io/kcrypt/pkg/lib"
	"path/filepath"
)

type Kcrypt struct{}

func (k Kcrypt) Run(c config.Config, _ v1.Spec) error {
	if len(c.Install.Encrypt) == 0 {
		return nil
	}
	c.Logger.Logger.Info().Msg("Running encrypt hook")

	// We need to unmount the persistent partition to encrypt it
	// we dont know the state here so we better try
	err := machine.Umount(filepath.Join("/dev/disk/by-label", constants.PersistentLabel)) //nolint:errcheck
	if err != nil {
		c.Logger.Errorf("could not unmount persistent partition: %s", err)
		return err
	}

	// Config passed during install ends up here, so we need to read it
	_ = machine.Mount("COS_OEM", "/oem")
	defer func() {
		_ = machine.Umount("/oem") //nolint:errcheck
	}()

	for _, p := range c.Install.Encrypt {
		_, err := kcrypt.Luksify(p, c.Logger.Logger)
		if err != nil {
			c.Logger.Errorf("could not encrypt partition: %s", err)
			if c.FailOnBundleErrors {
				return err
			}
		}
	}
	c.Logger.Logger.Info().Msg("Finished encrypt hook")
	return nil
}
