package agent

import (
	"encoding/json"
	"fmt"
	"github.com/sanity-io/litter"
	"github.com/sirupsen/logrus"
	"os"
	"sync"
	"time"

	sdk "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	hook "github.com/kairos-io/kairos/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos/v2/internal/bus"
	"github.com/kairos-io/kairos/v2/internal/cmd"
	"github.com/kairos-io/kairos/v2/pkg/action"
	"github.com/kairos-io/kairos/v2/pkg/config"
	"github.com/kairos-io/kairos/v2/pkg/elementalConfig"

	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
)

func Reset(debug bool, dir ...string) error {
	// TODO: Enable args? No args for now so no possibility of reset persistent or overriding the source for the reset
	// Nor the auto-reboot via cmd?
	// This comment pertains calling reset via cmdline when wanting to override configs
	bus.Manager.Initialize()

	options := map[string]string{}

	bus.Manager.Response(sdk.EventBeforeReset, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(r.Data), &options)
		if err != nil {
			fmt.Println(err)
		}
	})

	cmd.PrintBranding(DefaultBanner)

	// This loads yet another config ¬_¬
	// TODO: merge this somehow with the rest so there is no 5 places to configure stuff?
	// Also this reads the elemental config.yaml
	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

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

	ensureDataSourceReady()

	bus.Manager.Publish(sdk.EventBeforeReset, sdk.EventPayload{}) //nolint:errcheck

	c, err := config.Scan(collector.Directories(dir...))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	resetConfig, err := elementalConfig.ReadConfigRun("/etc/elemental")
	if err != nil {
		return err
	}
	if debug {
		resetConfig.Logger.SetLevel(logrus.DebugLevel)
	}
	resetConfig.Logger.Debugf("Full config: %s\n", litter.Sdump(resetConfig))
	resetSpec, err := elementalConfig.ReadResetSpec(resetConfig)
	if err != nil {
		return err
	}
	// Not even sure what opts can come from here to be honest. Where is the struct that supports this options?
	// Where is the docs to support this? This is generic af and not easily identifiable
	if len(options) == 0 {
		resetSpec.FormatPersistent = true
	} else {
		fmt.Println(options)
	}

	resetAction := action.NewResetAction(resetConfig, resetSpec)
	if err := resetAction.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := hook.Run(*c, hook.AfterReset...); err != nil {
		return err
	}

	bus.Manager.Publish(sdk.EventAfterReset, sdk.EventPayload{}) //nolint:errcheck

	if !agentConfig.Fast {
		pterm.Info.Println("Rebooting in 60 seconds, press Enter to abort...")
	}

	// We don't close the lock, as none of the following actions are expected to return
	lock2 := sync.Mutex{}
	go func() {
		// Wait for user input and go back to shell
		utils.Prompt("") //nolint:errcheck
		// give tty1 back
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint:errcheck
		}

		lock2.Lock()
		fmt.Println("Reboot aborted")
		panic(utils.Shell().Run())
	}()

	if !agentConfig.Fast {
		time.Sleep(60 * time.Second)
	}
	lock2.Lock()
	utils.Reboot()

	return nil
}
