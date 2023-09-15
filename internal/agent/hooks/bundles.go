package hook

import (
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/machine"
)

type BundleOption struct{}

func (b BundleOption) Run(c config.Config, _ v1.Spec) error {

	machine.Mount("COS_PERSISTENT", "/usr/local") //nolint:errcheck
	defer func() {
		machine.Umount("/usr/local") //nolint:errcheck
	}()

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	opts := c.Install.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}

	return nil
}

type BundlePostInstall struct{}

func (b BundlePostInstall) Run(c config.Config, _ v1.Spec) error {
	opts := c.Bundles.Options()
	err := bundles.RunBundles(opts...)
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	return nil
}
