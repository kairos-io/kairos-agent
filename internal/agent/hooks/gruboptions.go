package hook

import (
	"fmt"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"

	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/system"
)

type GrubOptions struct{}

func (b GrubOptions) Run(c config.Config, _ v1.Spec) error {
	if len(c.Install.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Debugf("Setting grub options: %s", c.Install.GrubOptions)
	err := system.Apply(system.SetGRUBOptions(c.Install.GrubOptions))
	if err != nil {
		fmt.Println(err)
	}
	return nil
}

type GrubPostInstallOptions struct{}

func (b GrubPostInstallOptions) Run(c config.Config, _ v1.Spec) error {
	err := system.Apply(system.SetGRUBOptions(c.GrubOptions))
	if err != nil {
		fmt.Println(err)
	}
	return nil
}
