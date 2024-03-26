package agent

import (
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/spf13/viper"
	"os"
)

// Run starts the agent provider emitting the bootstrap event.
func Run(opts ...Option) error {
	o := &Options{}
	if err := o.Apply(opts...); err != nil {
		return err
	}

	os.MkdirAll("/usr/local/.kairos", 0600) //nolint:errcheck

	// Reads config
	c, err := config.Scan(collector.Directories(o.Dir...))
	if err != nil {
		return err
	}
	// Recreate the logger with a different name
	c.Logger = sdkTypes.NewKairosLogger("agent-provider", "info", false)
	if viper.GetBool("debug") {
		c.Logger.SetLevel("debug")
	}

	utils.SetEnv(c.Env)
	bf := machine.BootFrom()
	if c.Install != nil && c.Install.Auto && (bf == machine.NetBoot || bf == machine.LiveCDBoot) {
		// Don't go ahead if we are asked to install from a booting live medium
		c.Logger.Info("Agent run aborted. Installation being performed from live medium")
		return nil
	}

	if !machine.SentinelExist("firstboot") {
		c.Logger.Info("First boot detected, running first boot hooks")
		spec := v1.EmptySpec{}
		if err := hook.Run(*c, &spec, hook.FirstBoot...); err != nil {
			c.Logger.Error("First boot hooks failed: ", err)
			return err
		}

		// Re-load providers
		bus.Reload()
		err = machine.CreateSentinel("firstboot")
		if c.FailOnBundleErrors && err != nil {
			c.Logger.Error("Failed to create firstboot sentinel: ", err)
			return err
		}

		// Re-read config files
		c, err = config.Scan(collector.Directories(o.Dir...))
		if err != nil {
			return err
		}
	}
	configStr, err := c.Config.String()
	if err != nil {
		panic(err)
	}
	_, err = bus.Manager.Publish(events.EventBootstrap, events.BootstrapPayload{APIAddress: o.APIAddress, Config: configStr})

	if o.Restart && err != nil {
		c.Logger.Warnf("Warning: Agent failed, restarting: %s", err.Error())
		return Run(opts...)
	}
	return err
}
