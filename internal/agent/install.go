package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sanity-io/litter"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	hook "github.com/kairos-io/kairos/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos/v2/internal/bus"
	"github.com/kairos-io/kairos/v2/internal/cmd"
	"github.com/kairos-io/kairos/v2/pkg/action"
	"github.com/kairos-io/kairos/v2/pkg/config"
	"github.com/kairos-io/kairos/v2/pkg/config/collector"
	"github.com/kairos-io/kairos/v2/pkg/elementalConfig"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos/v2/pkg/utils"
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

func ManualInstall(c string, options map[string]string, strictValidations, debug bool) error {
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
	configStr, err := cc.String()
	if err != nil {
		return err
	}
	options["cc"] = configStr
	// unlike Install device is already set
	// options["device"] = cc.Install.Device

	mergeOption(configStr, options)

	if options["device"] == "" {
		if cc.Install.Device == "" {
			options["device"] = detectDevice()
		} else {
			options["device"] = cc.Install.Device
		}
	}

	// Load the installation Config from the system
	installConfig, err := elementalConfig.ReadConfigRun("/etc/elemental")
	if err != nil {
		return err
	}
	installConfig.Debug = debug
	installConfig.Logger.Debugf("Full config: %s\n", litter.Sdump(installConfig))

	return RunInstall(installConfig, options)
}

func Install(debug bool, dir ...string) error {
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

	// Load the installation Config from the system
	installConfig, err := elementalConfig.ReadConfigRun("/etc/elemental")
	if err != nil {
		return err
	}
	installConfig.Debug = debug
	installConfig.Logger.Debugf("Full config: %s\n", litter.Sdump(installConfig))

	// Reads config, and if present and offline is defined,
	// runs the installation
	cc, err := config.Scan(collector.Directories(dir...), collector.MergeBootLine, collector.NoLogs)
	if err == nil && cc.Install != nil && cc.Install.Auto {
		configStr, err := cc.String()
		if err != nil {
			return err
		}
		r["cc"] = configStr
		r["device"] = cc.Install.Device
		mergeOption(configStr, r)

		err = RunInstall(installConfig, r)
		if err != nil {
			return err
		}

		svc, err := machine.Getty(1)
		if err == nil {
			svc.Start() //nolint:errcheck
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

	// merge any options defined in it
	mergeOption(cloudConfig, r)

	// now merge cloud config from system and
	// the one received from the agent-provider
	ccData := map[string]interface{}{}

	// make sure the config we write has at least the #cloud-config header,
	// if any other was defined beforeahead
	header := "#cloud-config"
	if hasHeader, head := config.HasHeader(configStr, ""); hasHeader {
		header = head
	}

	// What we receive take precedence over the one in the system. best-effort
	yaml.Unmarshal([]byte(configStr), &ccData) //nolint:errcheck
	if exists {
		yaml.Unmarshal([]byte(cloudConfig), &ccData) //nolint:errcheck
		if hasHeader, head := config.HasHeader(cloudConfig, ""); hasHeader {
			header = head
		}
	}

	out, err := yaml.Marshal(ccData)
	if err != nil {
		return fmt.Errorf("failed marshalling cc: %w", err)
	}

	r["cc"] = config.AddHeader(header, string(out))

	pterm.Info.Println("Starting installation")

	if err := RunInstall(installConfig, r); err != nil {
		return err
	}

	pterm.Info.Println("Installation completed, press enter to go back to the shell.")

	utils.Prompt("") //nolint:errcheck

	// give tty1 back
	svc, err := machine.Getty(1)
	if err == nil {
		svc.Start() //nolint: errcheck
	}

	return nil
}

func RunInstall(installConfig *v1.RunConfig, options map[string]string) error {
	if installConfig.Debug {
		installConfig.Logger.SetLevel(logrus.DebugLevel)
	}

	f, _ := os.CreateTemp("", "xxxx")
	defer os.RemoveAll(f.Name())

	cloudInit, ok := options["cc"]
	if !ok {
		fmt.Println("cloudInit must be specified among options")
		os.Exit(1)
	}

	// TODO: Drop this and make a more straighforward way of getting the cloud-init and options?
	c := &config.Config{}
	yaml.Unmarshal([]byte(cloudInit), c) //nolint:errcheck

	if c.Install == nil {
		c.Install = &config.Install{}
	}

	// TODO: Im guessing this was used to try to override elemental values from env vars
	// Does it make sense anymore? We can now expose the whole options of elemental directly
	env := append(c.Install.Env, c.Env...)
	utils.SetEnv(env)

	err := os.WriteFile(f.Name(), []byte(cloudInit), os.ModePerm)
	if err != nil {
		fmt.Printf("could not write cloud init: %s\n", err.Error())
		os.Exit(1)
	}

	_, reboot := options["reboot"]
	_, poweroff := options["poweroff"]
	if poweroff {
		c.Install.Poweroff = true
	}
	if reboot {
		c.Install.Reboot = true
	}

	// Generate the installation spec
	installSpec, _ := elementalConfig.ReadInstallSpec(installConfig)

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
	// Set the target device
	device, ok := options["device"]
	if !ok {
		fmt.Println("device must be specified among options")
		os.Exit(1)
	}

	if device == "auto" {
		device = detectDevice()
	}
	installSpec.Target = device
	// Check if values are correct
	err = installSpec.Sanitize()
	if err != nil {
		return err
	}

	// Add user's cloud-config (to run user defined "before-install" stages)
	installConfig.CloudInitPaths = append(installConfig.CloudInitPaths, installSpec.CloudInit...)

	// Run pre-install stage
	_ = elementalUtils.RunStage(&installConfig.Config, "kairos-install.pre", installConfig.Strict, installConfig.CloudInitPaths...)
	events.RunHookScript("/usr/bin/kairos-agent.install.pre.hook") //nolint:errcheck
	// Create the action
	installAction := action.NewInstallAction(installConfig, installSpec)
	// Run it
	if err := installAction.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return hook.Run(*c, hook.AfterInstall...)
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
