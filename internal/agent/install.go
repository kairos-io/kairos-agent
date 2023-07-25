package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/elementalConfig"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	qr "github.com/mudler/go-nodepair/qrcode"
	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
	"gopkg.in/yaml.v2"
)

func displayInfo(agentConfig *Config) {
	fmt.Println("--------------------------")
	fmt.Println("No providers found, dropping to a shell. \n -- For instructions on how to install manually, see: https://kairos.io/docs/installation/manual/")
	if !agentConfig.WebUI.Disable {
		if !agentConfig.WebUI.HasAddress() {
			ips := machine.LocalIPs()
			if len(ips) > 0 {
				fmt.Print("WebUI installer running at : ")
				for _, ip := range ips {
					fmt.Printf("%s%s ", ip, config.DefaultWebUIListenAddress)
				}
				fmt.Print("\n")
			}
		} else {
			fmt.Printf("WebUI installer running at : %s\n", agentConfig.WebUI.ListenAddress)
		}

		ifaces := machine.Interfaces()
		fmt.Printf("Network Interfaces: %s\n", strings.Join(ifaces, " "))
	}
}

func mergeOption(cloudConfig string, r map[string]string) {
	c := &config.Config{}
	yaml.Unmarshal([]byte(cloudConfig), c) //nolint:errcheck
	for k, v := range c.Options {
		if k == "cc" {
			continue
		}
		r[k] = v
	}
}

func ManualInstall(c, device string, reboot, poweroff, strictValidations bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source, err := prepareConfiguration(ctx, c)
	if err != nil {
		return err
	}

	cc, err := config.Scan(collector.Directories(source), collector.MergeBootLine, collector.StrictValidation(strictValidations))
	if err != nil {
		return err
	}

	if reboot {
		// Override from flags!
		cc.Install.Reboot = true
	}

	if poweroff {
		// Override from flags!
		cc.Install.Poweroff = true
	}
	if device != "" {
		// Override from flags!
		cc.Install.Device = device
	}
	return RunInstall(cc)
}

func Install(dir ...string) error {
	utils.OnSignal(func() {
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint:errcheck
		}
	}, syscall.SIGINT, syscall.SIGTERM)

	tk := ""
	r := map[string]string{}

	bus.Manager.Response(events.EventChallenge, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		tk = r.Data
	})

	bus.Manager.Response(events.EventInstall, func(p *pluggable.Plugin, resp *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(resp.Data), &r)
		if err != nil {
			fmt.Println(err)
		}
	})

	ensureDataSourceReady()

	// Reads config, and if present and offline is defined,
	// runs the installation
	cc, err := config.Scan(collector.Directories(dir...), collector.MergeBootLine, collector.NoLogs)
	if err == nil && cc.Install != nil && cc.Install.Auto {
		err = RunInstall(cc)
		if err != nil {
			return err
		}

		if cc.Install.Reboot == false && cc.Install.Poweroff == false {
			pterm.DefaultInteractiveContinue.Options = []string{}
			pterm.DefaultInteractiveContinue.Show("Installation completed, press enter to go back to the shell.")
			svc, err := machine.Getty(1)
			if err == nil {
				svc.Start() //nolint:errcheck
			}
		}

		return nil
	}
	if err != nil {
		fmt.Printf("- config not found in the system: %s", err.Error())
	}

	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

	// try to clear screen
	cmd.ClearScreen()
	cmd.PrintBranding(DefaultBanner)

	// If there are no providers registered, we enter a shell for manual installation
	// and print information about the webUI
	if !bus.Manager.HasRegisteredPlugins() {
		displayInfo(agentConfig)
		return utils.Shell().Run()
	}

	configStr, err := cc.String()
	if err != nil {
		return err
	}
	_, err = bus.Manager.Publish(events.EventChallenge, events.EventPayload{Config: configStr})
	if err != nil {
		return err
	}

	cmd.PrintText(agentConfig.Branding.Install, "Installation")

	if !agentConfig.Fast {
		time.Sleep(5 * time.Second)
	}

	if tk != "" {
		qr.Print(tk)
	}

	if _, err := bus.Manager.Publish(events.EventInstall, events.InstallPayload{Token: tk, Config: configStr}); err != nil {
		return err
	}

	if len(r) == 0 {
		return errors.New("no configuration, stopping installation")
	}

	// we receive a cloud config at this point
	cloudConfig, exists := r["cc"]
	if exists {
		yaml.Unmarshal([]byte(cloudConfig), cc)
	}

	pterm.Info.Println("Starting installation")

	if err := RunInstall(cc); err != nil {
		return err
	}

	if cc.Install.Reboot {
		pterm.Info.Println("Installation completed, powering off in 5 seconds.")

	}
	if cc.Install.Poweroff {
		pterm.Info.Println("Installation completed, rebooting in 5 seconds.")
	}

	if cc.Install.Reboot == false && cc.Install.Poweroff == false {
		pterm.DefaultInteractiveContinue.Show("Installation completed, press enter to go back to the shell.")

		utils.Prompt("") //nolint:errcheck

		// give tty1 back
		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint: errcheck
		}
	}

	return nil
}

func RunInstall(c *config.Config) error {
	if c.Install.Device == "" || c.Install.Device == "auto" {
		c.Install.Device = detectDevice()
	}

	// Load the installation Config from the system
	installConfig, installSpec, err := elementalConfig.ReadInstallConfigFromAgentConfig(c)
	if err != nil {
		return err
	}

	f, err := elementalUtils.TempFile(installConfig.Fs, "", "kairos-install-config-xxx.yaml")
	if err != nil {
		installConfig.Logger.Error("Error creating temporal file for install config: %s\n", err.Error())
		return err
	}
	defer os.RemoveAll(f.Name())

	ccstring, err := c.String()
	if err != nil {
		installConfig.Logger.Error("Error creating temporary file for install config: %s\n", err.Error())
		return err
	}
	err = os.WriteFile(f.Name(), []byte(ccstring), os.ModePerm)
	if err != nil {
		fmt.Printf("could not write cloud init to %s: %s\n", f.Name(), err.Error())
		return err
	}

	installSpec.NoFormat = c.Install.NoFormat

	// Set our cloud-init to the file we just created
	installSpec.CloudInit = append(installSpec.CloudInit, f.Name())
	// Get the source of the installation if we are overriding it
	if c.Install.Image != "" {
		imgSource, err := v1.NewSrcFromURI(c.Install.Image)
		if err != nil {
			return err
		}
		installSpec.Active.Source = imgSource
	}

	// Check if values are correct
	err = installSpec.Sanitize()
	if err != nil {
		return err
	}

	// Add user's cloud-config (to run user defined "before-install" stages)
	installConfig.CloudInitPaths = append(installConfig.CloudInitPaths, installSpec.CloudInit...)

	// Run pre-install stage
	_ = elementalUtils.RunStage(installConfig, "kairos-install.pre")
	events.RunHookScript("/usr/bin/kairos-agent.install.pre.hook") //nolint:errcheck
	// Create the action
	installAction := action.NewInstallAction(installConfig, installSpec)
	// Run it
	if err := installAction.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return hook.Run(*c, installSpec, hook.AfterInstall...)
}

func ensureDataSourceReady() {
	timeout := time.NewTimer(5 * time.Minute)
	ticker := time.NewTicker(500 * time.Millisecond)

	defer timeout.Stop()
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			fmt.Println("userdata configuration failed to load after 5m, ignoring.")
			return
		case <-ticker.C:
			if _, err := os.Stat("/run/.userdata_load"); os.IsNotExist(err) {
				return
			}
			fmt.Println("userdata configuration has not yet completed. (waiting for /run/.userdata_load to be deleted)")
		}
	}
}

func prepareConfiguration(ctx context.Context, source string) (string, error) {
	// if the source is not an url it is already a configuration path
	if u, err := url.Parse(source); err != nil || u.Scheme == "" {
		return source, nil
	}

	// create a configuration file with the source referenced
	f, err := os.CreateTemp(os.TempDir(), "kairos-install-*.yaml")
	if err != nil {
		return "", err
	}

	// defer cleanup until after parent is done
	go func() {
		<-ctx.Done()
		_ = os.RemoveAll(f.Name())
	}()

	cfg := config.Config{
		ConfigURL: source,
	}
	if err = yaml.NewEncoder(f).Encode(cfg); err != nil {
		return "", err
	}

	return f.Name(), nil
}
