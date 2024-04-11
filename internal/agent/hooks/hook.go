package hook

import (
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
)

type Interface interface {
	Run(c config.Config, spec v1.Spec) error
}

var AfterInstall = []Interface{
	&GrubOptions{}, // Set custom GRUB options
	&BundlePostInstall{},
	&CustomMounts{},
	&CopyLogs{},
	&Lifecycle{}, // Handles poweroff/reboot by config options
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
var AfterUkiInstall = []Interface{}

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
