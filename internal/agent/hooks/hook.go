package hook

import (
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
)

type Interface interface {
	Run(c config.Config, spec v1.Spec) error
}

var AfterInstall = []Interface{
	&RunStage{},    // Shells out to stages defined from the container image
	&GrubOptions{}, // Set custom GRUB options
	&BundleOption{},
	&CustomMounts{},
	&Kcrypt{},
	&Lifecycle{}, // Handles poweroff/reboot by config options
}

var AfterReset = []Interface{
	&Lifecycle{},
}

var AfterUpgrade = []Interface{
	&Lifecycle{},
}

var FirstBoot = []Interface{
	&BundlePostInstall{},
	&GrubPostInstallOptions{},
}

func Run(c config.Config, spec v1.Spec, hooks ...Interface) error {
	for _, h := range hooks {
		if err := h.Run(c, spec); err != nil {
			return err
		}
	}
	return nil
}
