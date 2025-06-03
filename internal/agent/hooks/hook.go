package hook

import (
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/utils"
	"strings"
)

type Interface interface {
	Run(c config.Config, spec v1.Spec) error
}

// FinishInstall is a list of hooks that run when the install process is finished completely.
// Its mean for options that are not related to the install process itself
var FinishInstall = []Interface{
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

// FinishReset is a list of hooks that run when the reset process is finished completely.
var FinishReset = []Interface{
	&CopyLogs{},  // Try to copy the reset logs to the persistent partition
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

// FinishUpgrade is a list of hooks that run when the upgrade process is finished completely.
var FinishUpgrade = []Interface{
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

// FirstBoot is a list of hooks that run on the first boot of the node.
var FirstBoot = []Interface{
	&BundleFirstBoot{},
	&GrubFirstBootOptions{},
}

// FinishUKIInstall is a list of hooks that run when the install process is finished completely.
// Its mean for options that are not related to the install process itself
var FinishUKIInstall = []Interface{
	&SysExtPostInstall{}, // Installs sysexts into the EFI partition
	&Lifecycle{},         // Handles poweroff/reboot by config options
}

// PostInstall is a list of hooks that run after the install process has run.
// Runs things that need to be done before we run other post install stages like
// encrypting partitions, copying the install logs or installing bundles
// Most of this options are optional so they are not run by default unless specified in the config
var PostInstall = []Interface{
	&Finish{},
}

func Run(c config.Config, spec v1.Spec, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.Run(c, spec); err != nil {
			return err
		}
	}
	return nil
}

// lockPartitions will try to close all the partitions that are unencrypted.
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
