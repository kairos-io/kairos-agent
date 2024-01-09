package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/uki"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/sanity-io/litter"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
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

func ManualInstall(c, sourceImgURL, device string, reboot, poweroff, strictValidations bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	configSource, err := prepareConfiguration(ctx, c)
	if err != nil {
		return err
	}

	cliConf := generateInstallConfForCLIArgs(sourceImgURL)
	cliConfManualArgs := generateInstallConfForManualCLIArgs(device, reboot, poweroff)

	cc, err := config.Scan(collector.Directories(configSource),
		collector.Readers(strings.NewReader(cliConf), strings.NewReader(cliConfManualArgs)),
		collector.MergeBootLine,
		collector.StrictValidation(strictValidations), collector.NoLogs)
	if err != nil {
		return err
	}

	return RunInstall(cc)
}

func Install(sourceImgURL string, dir ...string) error {
	var cc *config.Config
	var err error

	bus.Manager.Initialize()
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
		cloudConfig, exists := r["cc"]
		if exists {
			// Re-read the full config and add the config coming from the event
			cc, _ = config.Scan(collector.Directories(dir...), collector.Readers(strings.NewReader(cloudConfig)), collector.MergeBootLine, collector.NoLogs)
		}
	})

	ensureDataSourceReady()

	cliConf := generateInstallConfForCLIArgs(sourceImgURL)

	// Reads config, and if present and offline is defined, runs the installation
	cc, err = config.Scan(collector.Directories(dir...),
		collector.Readers(strings.NewReader(cliConf)),
		collector.MergeBootLine)
	if err == nil && cc.Install != nil && cc.Install.Auto {
		err = RunInstall(cc)
		if err != nil {
			return err
		}

		if cc.Install.Reboot == false && cc.Install.Poweroff == false {
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
		// This means there is no config in the system AND no config was obtained from events
		return errors.New("no configuration, stopping installation")
	}
	pterm.Info.Println("Starting installation")

	cc.Logger.Debugf("Runinstall with cc: %s\n", litter.Sdump(cc))
	if err := RunInstall(cc); err != nil {
		return err
	}

	if cc.Install.Reboot {
		pterm.Info.Println("Installation completed, rebooting in 5 seconds.")

	}
	if cc.Install.Poweroff {
		pterm.Info.Println("Installation completed, powering in 5 seconds.")
	}

	// If neither reboot and poweroff are enabled let the user insert enter to go back to a new shell
	// This is helpful to see the installation messages instead of just cleaning the screen with a new tty
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
	utils.SetEnv(c.Env)
	utils.SetEnv(c.Install.Env)

	if c.Install.Device == "" || c.Install.Device == "auto" {
		c.Install.Device = detectDevice()
	}

	// UKI path. Check if we are on UKI AND if we are running off a cd, otherwise it makes no sense to run the install
	// From the installed system
	if internalutils.IsUki() && internalutils.UkiBootMode() == internalutils.UkiRemovableMedia {
		// Load the spec from the config
		installSpec, err := config.ReadUkiInstallSpecFromConfig(c)
		if err != nil {
			return err
		}

		f, err := fsutils.TempFile(c.Fs, "", "kairos-install-config-xxx.yaml")
		if err != nil {
			c.Logger.Error("Error creating temporary file for install config: %s\n", err.Error())
			return err
		}
		defer os.RemoveAll(f.Name())

		ccstring, err := c.String()
		if err != nil {
			return err
		}
		err = os.WriteFile(f.Name(), []byte(ccstring), os.ModePerm)
		if err != nil {
			fmt.Printf("could not write cloud init to %s: %s\n", f.Name(), err.Error())
			return err
		}

		// Add user's cloud-config (to run user defined "before-install" stages)
		c.CloudInitPaths = append(c.CloudInitPaths, installSpec.CloudInit...)

		// PRE and POST install hooks are done inside the action for UKI
		installAction := uki.NewInstallAction(c, installSpec)
		return installAction.Run()
	} else { // Non-uki path
		// Load the installation spec from the Config
		installSpec, err := config.ReadInstallSpecFromConfig(c)
		if err != nil {
			return err
		}

		f, err := fsutils.TempFile(c.Fs, "", "kairos-install-config-xxx.yaml")
		if err != nil {
			c.Logger.Error("Error creating temporary file for install config: %s\n", err.Error())
			return err
		}
		defer os.RemoveAll(f.Name())

		ccstring, err := c.String()
		if err != nil {
			return err
		}
		err = os.WriteFile(f.Name(), []byte(ccstring), os.ModePerm)
		if err != nil {
			fmt.Printf("could not write cloud init to %s: %s\n", f.Name(), err.Error())
			return err
		}

		// TODO: This should not be neccessary
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
		c.CloudInitPaths = append(c.CloudInitPaths, installSpec.CloudInit...)

		// Create the action
		installAction := action.NewInstallAction(c, installSpec)
		// Run it
		return installAction.Run()
	}
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

func generateInstallConfForCLIArgs(sourceImageURL string) string {
	if sourceImageURL == "" {
		return ""
	}

	return fmt.Sprintf(`install:
  system:
    uri: %s
`, sourceImageURL)
}

// generateInstallConfForManualCLIArgs creates a kairos configuration for flags passed via manual install
func generateInstallConfForManualCLIArgs(device string, reboot, poweroff bool) string {
	cfg := fmt.Sprintf(`install:
  reboot: %t
  poweroff: %t
`, reboot, poweroff)

	if device != "" {
		cfg += fmt.Sprintf(`
  device: %s
`, device)
	}
	return cfg
}
