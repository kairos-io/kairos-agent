package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	sdk "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"

	"github.com/mudler/go-pluggable"
)

func Reset(reboot, unattended bool, dir ...string) error {
	bus.Manager.Initialize()

	// This config is only for reset branding.
	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

	if !unattended {
		cmd.PrintBranding(DefaultBanner)
		cmd.PrintText(agentConfig.Branding.Reset, "Reset")

		// We don't close the lock, as none of the following actions are expected to return
		lock := sync.Mutex{}
		go func() {
			// Wait for user input and go back to shell
			utils.Prompt("") //nolint:errcheck
			// give tty1 back
			svc, err := machine.Getty(1)
			if err == nil {
				svc.Start() //nolint:errcheck
			}

			lock.Lock()
			fmt.Println("Reset aborted")
			panic(utils.Shell().Run())
		}()

		if !agentConfig.Fast {
			time.Sleep(60 * time.Second)
		}

		lock.Lock()
	}

	ensureDataSourceReady()

	optionsFromEvent := map[string]string{}

	// This gets the options from an event that can be sent by anyone.
	// This should override the default config as it's much more dynamic
	bus.Manager.Response(sdk.EventBeforeReset, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(r.Data), &optionsFromEvent)
		if err != nil {
			fmt.Println(err)
		}
	})

	bus.Manager.Publish(sdk.EventBeforeReset, sdk.EventPayload{}) //nolint:errcheck

	c, err := config.Scan(collector.Directories(dir...))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	// Load the installation Config from the cloud-config data
	resetSpec, err := config.ReadResetSpecFromConfig(c)
	if err != nil {
		return err
	}

	// Go over the possible options sent via event
	if len(optionsFromEvent) > 0 {
		if p := optionsFromEvent["reset-persistent"]; p != "" {
			resetSpec.FormatPersistent = p == "true"
		}
		if o := optionsFromEvent["reset-oem"]; o != "" {
			resetSpec.FormatOEM = o == "true"
		}
		if s := optionsFromEvent["strict"]; s != "" {
			c.Strict = s == "true"
		}
	}

	// Override with flags
	if reboot {
		resetSpec.Reboot = reboot
	}

	resetAction := action.NewResetAction(c, resetSpec)
	if err := resetAction.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	bus.Manager.Publish(sdk.EventAfterReset, sdk.EventPayload{}) //nolint:errcheck

	return hook.Run(*c, resetSpec, hook.AfterReset...)
}
