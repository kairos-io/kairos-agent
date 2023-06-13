package agent

import (
	"fmt"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	hook "github.com/kairos-io/kairos/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos/v2/internal/bus"
	config "github.com/kairos-io/kairos/v2/pkg/config"
	"github.com/kairos-io/kairos/v2/pkg/config/collector"
	"github.com/nxadm/tail"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
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

	utils.SetEnv(c.Env)
	bf := machine.BootFrom()
	if c.Install != nil && c.Install.Auto && (bf == machine.NetBoot || bf == machine.LiveCDBoot) {
		// Don't go ahead if we are asked to install from a booting live medium
		fmt.Println("Agent run aborted. Installation being performed from live medium")
		// This needs to act like a daemon, so once we reach this point we cannot just exit, we need to stay active
		// so wait for signals
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc,
			syscall.SIGHUP,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT)
		fmt.Println("Listening for signals")
		<-sigc
		return nil
	}

	os.MkdirAll("/var/log/kairos", 0600) //nolint:errcheck

	fileName := filepath.Join("/var/log/kairos", "agent-provider.log")

	// Create if not exist
	if _, err := os.Stat(fileName); err != nil {
		err = os.WriteFile(fileName, []byte{}, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Tail to the log
	t, err := tail.TailFile(fileName, tail.Config{Follow: true})
	if err != nil {
		return err
	}

	go func() {
		for line := range t.Lines {
			fmt.Println(line.Text)
		}
	}()

	if !machine.SentinelExist("firstboot") {

		if err := hook.Run(*c, hook.FirstBoot...); err != nil {
			return err
		}

		// Re-load providers
		bus.Reload()
		err = machine.CreateSentinel("firstboot")
		if c.FailOnBundleErrors && err != nil {
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
	_, err = bus.Manager.Publish(events.EventBootstrap, events.BootstrapPayload{APIAddress: o.APIAddress, Config: configStr, Logfile: fileName})

	if o.Restart && err != nil {
		fmt.Println("Warning: Agent failed, restarting: ", err.Error())
		return Run(opts...)
	}
	// Same here, we got no error, so we stay alive
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)
	fmt.Println("Listening for signals")
	<-sigc
	return err
}
