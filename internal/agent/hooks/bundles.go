package hook

import (
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
	// system extension are now installed to /var/lib/extensions
	// https://github.com/kairos-io/kairos/issues/1821
	// so if we want them to work as expected we need to mount the persistent dir and the bind dir that will
	// then end up in the system as /usr/lib/extensions is not valid anymore
	// So after the install from livecd we need to:
	// - mount persistent in /usr/local
	// - create the dir that will be binded into /var/lib/extensions after reboot in /usr/local/.state/var-lib-extensions.bind
	// - rsync whatever is in the /var/lib/extensions to /usr/local/.state/var-lib-extensions.bind so we dont lost thing shipped with the install media
	// - bind the dir so the extension gets persistent after reboots
	// - umount the bind dir
	// Note that the binding of /usr/local/.state/var-lib-extensions.bind to /var/lib/extensions on active/passive its done by inmmucore based on the
	// 00_rootfs.yaml config which sets the bind and ephemeral paths.

	machine.Mount("COS_PERSISTENT", "/usr/local") //nolint:errcheck
	defer func() {
		machine.Umount("/usr/local") //nolint:errcheck
	}()

	err := os.MkdirAll("/usr/local/.state/var-lib-extensions.bind", os.ModeDir|os.ModePerm)
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	cmd := exec.Command("rsync -aqAX /var/lib/extensions /usr/local/.state/var-lib-extensions.bind")
	_, err = cmd.CombinedOutput()
	if c.FailOnBundleErrors && err != nil {
		return err
	}
	err = syscall.Mount("/usr/local/.state/var-lib-extensions.bind", "/var/lib/extensions", "", syscall.MS_BIND, "")
	if c.FailOnBundleErrors && err != nil {
		return err
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
