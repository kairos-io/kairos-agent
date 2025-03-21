package hook

import (
	"fmt"
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/utils"
	"strings"
)

type Interface interface {
	Run(c config.Config, spec v1.Spec) error
}

var AfterInstall = []Interface{
	&GrubOptions{}, // Set custom GRUB options
	&Lifecycle{},   // Handles poweroff/reboot by config options
}

var AfterReset = []Interface{
	&CopyLogs{},
	&Lifecycle{},
}

var AfterUpgrade = []Interface{
	&Lifecycle{},
}

var FirstBoot = []Interface{
	&BundleFirstBoot{},
	&GrubPostInstallOptions{},
}

// AfterUkiInstall sets which Hooks to run after uki runs the install action
var AfterUkiInstall = []Interface{
	&SysExtPostInstall{},
	&Lifecycle{},
}

var UKIEncryptionHooks = []Interface{
	&KcryptUKI{},
}

var EncryptionHooks = []Interface{
	&Kcrypt{},
}

func Run(c config.Config, spec v1.Spec, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.Run(c, spec); err != nil {
			return err
		}
	}
	return nil
}

// lockPartitions will try to close all the partitions that are unencrypted
func lockPartitions(c config.Config) {
	for _, p := range c.Install.Encrypt {
		_, _ = utils.SH("udevadm trigger --type=all || udevadm trigger")
		c.Logger.Debugf("Closing unencrypted /dev/disk/by-label/%s", p)
		out, err := utils.SH(fmt.Sprintf("cryptsetup close /dev/disk/by-label/%s", p))
		// There is a known error with cryptsetup that it can't close the device because of a semaphore
		// doesnt seem to affect anything as the device is closed as expected so we ignore it if it matches the
		// output of the error
		if err != nil && !strings.Contains(out, "incorrect semaphore state") {
			c.Logger.Debugf("could not close /dev/disk/by-label/%s: %s", p, out)
		}
	}
}
