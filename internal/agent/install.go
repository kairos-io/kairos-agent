package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/loop"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/uki"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"

	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/sanity-io/litter"

	qr "github.com/kairos-io/go-nodepair/qrcode"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/mudler/go-pluggable"
	"github.com/pterm/pterm"
)

func displayInfo(agentConfig *Config) {
	if !agentConfig.WebUI.Disable {
		ifaces := machine.Interfaces()
		message := fmt.Sprintf("Interfaces: %s", strings.Join(ifaces, " "))
		if !agentConfig.WebUI.HasAddress() {
			ips := machine.LocalIPs()
			if len(ips) > 0 {
				messageIps := " - WebUI installer: "
				for _, ip := range ips {
					// Skip printing local ips, makes no sense
					if strings.Contains("127.0.0.1", ip) || strings.Contains("::1", ip) {
						continue
					}
					messageIps = messageIps + fmt.Sprintf("%s%s ", ip, config.DefaultWebUIListenAddress)
				}
				message = message + messageIps
			}
		} else {
			message = message + fmt.Sprintf(" - WebUI installer: %s", agentConfig.WebUI.ListenAddress)
		}
		fmt.Println(message)
	}
}

func ManualInstall(c, sourceImgURL, device string, reboot, poweroff, strictValidations bool) error {
	configSource, err := prepareConfiguration(c)
	if err != nil {
		return err
	}

	cliConf := generateInstallConfForCLIArgs(sourceImgURL)
	cliConfManualArgs := generateInstallConfForManualCLIArgs(device, reboot, poweroff)

	cc, err := config.Scan(
		collector.Readers(configSource, strings.NewReader(cliConf), strings.NewReader(cliConfManualArgs)),
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
			cc, _ = config.Scan(collector.Directories(dir...), collector.Overwrites(cloudConfig), collector.MergeBootLine, collector.NoLogs)
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

		if !cc.Install.Reboot && !cc.Install.Poweroff {
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
		fmt.Println("No providers found, dropping to a shell. \n -- For instructions on how to install manually, see: https://kairos.io/docs/installation/manual/")
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
		displayInfo(agentConfig)
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
		pterm.Info.Println("Installation completed, starting reboot in 5 seconds.")

	}
	if cc.Install.Poweroff {
		pterm.Info.Println("Installation completed, starting power off in 5 seconds.")
	}

	// If neither reboot and poweroff are enabled let the user insert enter to go back to a new shell
	// This is helpful to see the installation messages instead of just cleaning the screen with a new tty
	if !cc.Install.Reboot && !cc.Install.Poweroff {
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

	err := c.CheckForUsers()
	if err != nil {
		return err
	}

	// UKI path. Check if we are on UKI AND if we are running off a cd, otherwise it makes no sense to run the install
	// From the installed system
	if internalutils.IsUkiWithFs(c.Fs) {
		c.Logger.Debugf("UKI mode: %s\n", internalutils.UkiBootMode())
		if internalutils.UkiBootMode() == internalutils.UkiRemovableMedia {
			return runInstallUki(c)
		}
		c.Logger.Warnf("UKI boot mode is not removable media, skipping install")
		return nil
	} else { // Non-uki path
		return runInstall(c)
	}
}

// runInstallUki runs the UKI path install
func runInstallUki(c *config.Config) error {
	// Check if we are running in PXE
	efivar, err := internalutils.ReadEfivar(constants.PXEVarFile)
	// We dont care if we fail to read it, that means its not there
	if err == nil {
		c.Logger.Logger.Info().Str("iso", string(efivar)).Msg("PXE boot detected, downloading and mounting the iso locally")
		err = c.Client.GetURL(c.Logger, string(efivar), constants.PXEIsoFile)
		if err != nil {
			return err
		}

		isoLoop, err := loop.LoopRO(&v1.Image{File: constants.PXEIsoFile}, c)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Error creating loop device for iso image")
			return err
		}
		defer loop.Unloop(isoLoop, c)
		c.Logger.Logger.Debug().Str("iso", isoLoop).Msg("Mounted iso loop device")

		// Mount the iso under /run/initramfs/live
		err = c.Mounter.Mount(isoLoop, constants.UkiCdromSource, constants.IsoFS, nil)
		if err != nil {
			c.Logger.Errorf("Error mounting iso: %s", err.Error())
			return err
		}
		c.Logger.Infof("Mounted iso under %s", constants.UkiCdromSource)
		defer c.Mounter.Unmount(constants.UkiCdromSource)

		// Now mount the efi image inside the iso
		efiLoop, err := loop.LoopRO(&v1.Image{File: filepath.Join(constants.UkiCdromSource, constants.PXEEfiBootFile)}, c)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Error creating loop device for efi image")
			return err
		}
		defer loop.Unloop(efiLoop, c)
		c.Logger.Logger.Debug().Str("efi", efiLoop).Msg("Mounted efi loop device")

		// Mount the efi image under /run/rootfsbase which is the same as to other boot paths mount it at
		err = c.Mounter.Mount(efiLoop, constants.IsoBaseTree, constants.EfiFs, nil)
		if err != nil {
			c.Logger.Errorf("Error mounting iso: %s", err.Error())
			return err
		}
		c.Logger.Infof("Mounted Efi source under %s", constants.IsoBaseTree)
		defer c.Mounter.Unmount(constants.IsoBaseTree)
		// Now the system should have the same paths and sources as the normal install from usb/cdrom
	}

	// Load the spec from the config
	installSpec, err := config.ReadUkiInstallSpecFromConfig(c)
	if err != nil {
		return err
	}

	// Set our cloud-init to the file we just created
	f, err := dumpCCStringToFile(c)
	if err == nil {
		installSpec.CloudInit = append(installSpec.CloudInit, f)
	}

	// Check if values are correct
	err = installSpec.Sanitize()
	if err != nil {
		return err
	}

	// Add user's cloud-config (to run user defined "before-install" stages)
	c.CloudInitPaths = append(c.CloudInitPaths, installSpec.CloudInit...)

	installAction := uki.NewInstallAction(c, installSpec)
	err = installAction.Run()

	if err == nil && utils.Exists(constants.PXEVarFile) {
		// TODO: do we fail here?
		err = internalutils.RemoveEfivarPXE(c.Logger)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Error removing PXE Efivar")
			return err
		}
		// Now remove the boot entry
		// TODO: Do we fail here?
		err = internalutils.RemoveBootEntry("kairos", c.Logger)
		if err != nil {
			c.Logger.Logger.Error().Err(err).Msg("Error removing PXE boot entry")
			return err
		}
	}
	return err
}

// runInstall runs the non-UKI path install
func runInstall(c *config.Config) error {
	// Load the installation spec from the Config
	installSpec, err := config.ReadInstallSpecFromConfig(c)
	if err != nil {
		return err
	}

	// Set our cloud-init to the file we just created
	f, err := dumpCCStringToFile(c)
	if err == nil {
		installSpec.CloudInit = append(installSpec.CloudInit, f)
	}

	// Check if values are correct
	err = installSpec.Sanitize()
	if err != nil {
		return err
	}

	// Add user's cloud-config (to run user defined "before-install" stages)
	c.CloudInitPaths = append(c.CloudInitPaths, installSpec.CloudInit...)

	installAction := action.NewInstallAction(c, installSpec)
	return installAction.Run()
}

// dumpCCStringToFile dumps the cloud-init string to a file and returns the path of the file
func dumpCCStringToFile(c *config.Config) (string, error) {
	f, err := fsutils.TempFile(c.Fs, "", "kairos-install-config-xxx.yaml")
	if err != nil {
		c.Logger.Errorf("Error creating temporary file for install config: %s", err.Error())
		return "", err
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)
	ccstring, err := c.String()
	if err != nil {
		return "", err
	}
	err = os.WriteFile(f.Name(), []byte(ccstring), os.ModePerm)
	if err != nil {
		fmt.Printf("could not write cloud init to %s: %s\n", f.Name(), err.Error())
		return "", err
	}
	return f.Name(), nil
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

func prepareConfiguration(source string) (io.Reader, error) {
	var cfg io.Reader
	// source can be either a file in the system or an url
	// We need to differentiate between the two
	// If its a local file, we just read it and return it
	// If its a url, we need to create a configuration with the url and let the config.Scan handle it
	// if the source is not an url it is already a configuration path
	if u, err := url.Parse(source); err != nil || u.Scheme == "" {
		file, err := os.ReadFile(source)
		if err != nil {
			return cfg, err
		}
		cfg = bytes.NewReader(file)
		return cfg, nil
	}
	// Its a remote url
	// Check if it actually exists and fail if it doesn't
	resp, err := http.Head(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, errors.New("configuration file not found in remote address")
		} else {
			return nil, errors.New(resp.Status)
		}
	}

	cfgUrl := fmt.Sprintf(`config_url: %s`, source)
	cfg = strings.NewReader(cfgUrl)

	return cfg, nil
}

func generateInstallConfForCLIArgs(sourceImageURL string) string {
	if sourceImageURL == "" {
		return ""
	}

	return fmt.Sprintf(`install:
  source: %s
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
