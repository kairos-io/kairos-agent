package hook

import (
	"fmt"
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/machine"
	"os"
	"os/exec"
	"syscall"
)

type BundleOption struct{}

func (b BundleOption) Run(c config.Config, _ v1.Spec) error {

	machine.Mount("COS_PERSISTENT", "/usr/local") //nolint:errcheck
	defer func() {
		machine.Umount("/usr/local") //nolint:errcheck
	}()

	_ = os.MkdirAll("/usr/local/var-lib-extensions.bind", os.ModeDir|os.ModePerm)
	cmd := exec.Command("rsync -aqAX /var/lib/extensions /usr/local/var-lib-extensions.bind")
	_, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println(err)
	}
	err = syscall.Mount("/usr/local/var-lib-extensions.bind", "/var/lib/extensions", "", syscall.MS_BIND, "")
	if err != nil {
		fmt.Println(err)
	}
	defer func() {
		_ = syscall.Unmount("/var/lib/extensions", 0)
	}()

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	opts := c.Install.Bundles.Options()
	err = bundles.RunBundles(opts...)
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
