package agent

import (
	"fmt"
	"time"

	qr "github.com/kairos-io/go-nodepair/qrcode"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
)

// recoveryPromptFn and recoveryFastSleep are package-level indirections so
// tests can bypass the blocking prompt and 5-second wait without spinning up
// a real TTY. Production behaviour is unchanged.
var (
	recoveryPromptFn  = func(s string) (string, error) { return utils.Prompt(s) }
	recoveryFastSleep = func(d time.Duration) { time.Sleep(d) }
)

// startGettyTTY1 restores tty1 by starting getty@tty1 after an interactive
// operation completes. It is a package-level indirection so tests can override
// it — on a systemd host machine.Getty(1).Start() actually runs
// `systemctl start getty@tty1.service`, which would switch away from the
// developer's active TTY and disrupt the running session. Tests override this
// via init() in the _test.go setup file to guarantee no test path ever touches
// the host's real getty service. Production behaviour is unchanged.
var startGettyTTY1 = func() {
	svc, err := machine.Getty(1)
	if err == nil {
		_ = svc.Start()
	}
}

func Recovery() error {
	bus.Manager.Initialize()

	token := ""
	msg := ""
	busErr := ""

	bus.Manager.Response(events.EventRecovery, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		token = r.Data
		msg = r.State
		busErr = r.Error
	})

	cmd.PrintBranding(DefaultBanner)

	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

	cmd.PrintText(agentConfig.Branding.Recovery, "Recovery")

	_, err = bus.Manager.Publish(events.EventRecovery, events.EventPayload{})
	if err != nil {
		return err
	}

	if busErr != "" {
		return fmt.Errorf("%s", busErr)
	}

	if !agentConfig.Fast {
		recoveryFastSleep(5 * time.Second)
	}

	pterm.Info.Println(msg)

	if token != "" {
		qr.Print(token)
	}

	// Wait for user input and go back to shell
	_, _ = recoveryPromptFn("")
	_, err = bus.Manager.Publish(events.EventRecoveryStop, events.EventPayload{})
	if err != nil {
		return err
	}
	// give tty1 back
	startGettyTTY1()

	return nil
}
