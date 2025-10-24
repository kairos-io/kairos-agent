package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
)

// Finish is a hook that runs after the install process.
// It is used to encrypt partitions and run the BundlePostInstall, CustomMounts and CopyLogs hooks
type Finish struct{}

func (k Finish) Run(c config.Config, spec v1.Spec) error {
	var err error

	// Copy cloud-config to OEM before encryption
	// The encryption process will backup and restore OEM, preserving the cloud-config
	if err := copyCloudConfigToOEM(c); err != nil {
		return err
	}

	// Run encryption (handles both UKI and non-UKI, returns early if nothing to encrypt)
	err = Encrypt(c)
	defer lockPartitions(c) // partitions are unlocked, make sure to lock them before we end
	if err != nil {
		c.Logger.Logger.Error().Err(err).Msg("could not encrypt partitions")
		return err
	}

	// Now that we have everything encrypted and ready to mount if needed
	err = GrubPostInstallOptions{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("Could not set grub options post install")
		return err
	}
	err = BundlePostInstall{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not copy run bundles post install")
		if c.FailOnBundleErrors {
			return err
		}
	}
	err = CustomMounts{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not create custom mounts")
	}
	err = CopyLogs{}.Run(c, spec)
	if err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("could not copy logs")
	}
	return nil
}
