package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/boards"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/agent"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/common"
	"github.com/kairos-io/kairos-agent/v2/internal/webui"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/uki"
	"github.com/kairos-io/kairos-sdk/bundles"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/schema"
	"github.com/kairos-io/kairos-sdk/state"
	"github.com/sirupsen/logrus"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/viper"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"
)

var configScanDir = []string{"/oem", "/usr/local/cloud-config", "/run/initramfs/live"}

// ReleasesToOutput gets a semver.Collection and outputs it in the given format
// Only used here.
func ReleasesToOutput(rels semver.Collection, output string) []string {
	// Set them back to their original version number with the v in front
	var stringRels []string
	for _, v := range rels {
		stringRels = append(stringRels, v.Original())
	}
	switch strings.ToLower(output) {
	case "yaml":
		d, _ := yaml.Marshal(stringRels)
		return []string{string(d)}
	case "json":
		d, _ := json.Marshal(stringRels)
		return []string{string(d)}
	default:
		return stringRels
	}
}

var sourceFlag = cli.StringFlag{
	Name:  "source",
	Usage: "Source for upgrade. Composed of `type:address`. Accepts `file:`,`dir:` or `oci:` for the type of source.\nFor example `file:/var/share/myimage.tar`, `dir:/tmp/extracted` or `oci:repo/image:tag`",
}

var cmds = []*cli.Command{
	{
		Name: "upgrade",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Force an upgrade",
			},
			&cli.StringFlag{
				Name:  "image",
				Usage: "[DEPRECATED] Specify a full image reference, e.g.: quay.io/some/image:tag",
			},
			&sourceFlag,
			&cli.BoolFlag{Name: "pre", Usage: "Include pre-releases (rc, beta, alpha)"},
			&cli.BoolFlag{Name: "recovery", Usage: "Upgrade recovery"},
		},
		Description: `
Manually upgrade a kairos node Active image. Does not upgrade passive or recovery images.

By default takes no arguments, defaulting to latest available release, to specify a version, pass it as argument:

$ kairos upgrade v1.20....

To retrieve all the available versions, use "kairos upgrade list-releases"

$ kairos upgrade list-releases

See https://kairos.io/docs/upgrade/manual/ for documentation.

`,
		Subcommands: []*cli.Command{
			{
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "output",
						Usage: "Output format (json|yaml|terminal)",
					},
					&cli.BoolFlag{Name: "pre", Usage: "Include pre-releases (rc, beta, alpha)"},
				},
				Name:        "list-releases",
				Description: `List all available releases versions`,
				Action: func(c *cli.Context) error {
					releases := agent.ListReleases(c.Bool("pre"))
					list := ReleasesToOutput(releases, c.String("output"))
					for _, i := range list {
						fmt.Println(i)
					}

					return nil
				},
			},
		},
		Before: func(c *cli.Context) error {
			if err := validateSource(c.String("source")); err != nil {
				return err
			}

			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			var v string
			if c.Args().Len() == 1 {
				v = c.Args().First()
			}

			image := c.String("image")
			source := c.String("source")

			if image != "" {
				fmt.Println("--image flag is deprecated, please use --source")
				// override source with image for now until we drop it
				source = fmt.Sprintf("oci:%s", image)
			}

			return agent.Upgrade(
				v, source, c.Bool("force"),
				c.Bool("strict-validation"), configScanDir,
				c.Bool("pre"), c.Bool("recovery"),
			)
		},
	},
	{
		Name:      "notify",
		Usage:     "notify <event> <config dir>...",
		UsageText: "emits the given event with a generic event payload",
		Description: `
Sends a generic event payload with the configuration found in the scanned directories.
`,
		Aliases: []string{},
		Flags:   []cli.Flag{},
		Action: func(c *cli.Context) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			if c.Args().Len() > 1 {
				dirs = c.Args().Slice()[1:]
			}

			return agent.Notify(c.Args().First(), dirs)
		},
	},
	{
		Name:      "start",
		Usage:     "Starts the kairos agent",
		UsageText: "starts the agent",
		Description: `
Starts the kairos agent which automatically bootstrap and advertize to the kairos network.
`,
		Aliases: []string{"s"},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "restart",
			},
			&cli.BoolFlag{
				Name: "force",
			},
			&cli.StringFlag{
				Name:  "api",
				Value: "http://127.0.0.1:8080",
			},
		},
		Action: func(c *cli.Context) error {
			dirs := []string{"/oem", "/usr/local/cloud-config"}
			if c.Args().Present() {
				dirs = c.Args().Slice()
			}

			opts := []agent.Option{
				agent.WithAPI(c.String("api")),
				agent.WithDirectory(dirs...),
			}

			if c.Bool("force") {
				opts = append(opts, agent.ForceAgent)
			}

			if c.Bool("restart") {
				opts = append(opts, agent.RestartAgent)
			}

			return agent.Run(opts...)
		},
	},
	{
		Name:  "install-bundle",
		Usage: "Installs a kairos bundle",
		Description: `

Manually installs a kairos bundle.

E.g. kairos-agent install-bundle container:quay.io/kairos/kairos...

`,
		Aliases: []string{"i"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repository",
				EnvVars: []string{"REPOSITORY"},
				Value:   "docker://quay.io/kairos/packages",
			},
			&cli.StringFlag{
				Name:  "root-path",
				Value: "/",
			},
			&cli.BoolFlag{
				Name:    "local-file",
				EnvVars: []string{"LOCAL_FILE"},
			},
		},
		UsageText: "Install a bundle manually in the node",
		Before: func(c *cli.Context) error {
			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			if c.Args().Len() != 1 {
				return fmt.Errorf("bundle name required")
			}

			return bundles.RunBundles([]bundles.BundleOption{bundles.WithRootFS(c.String("root-path")), bundles.WithRepository(c.String("repository")), bundles.WithTarget(c.Args().First()), bundles.WithLocalFile(c.Bool("local-file"))})
		},
	},
	{
		Name:        "uuid",
		Usage:       "Prints the local UUID",
		Description: "Print node uuid",
		Aliases:     []string{"u"},
		Action: func(c *cli.Context) error {
			fmt.Print(machine.UUID())
			return nil
		},
	},
	{
		Name:        "webui",
		Usage:       "Starts the webui",
		Description: "Starts the webui installer",
		Aliases:     []string{"w"},
		Action: func(c *cli.Context) error {
			return webui.Start(context.Background())
			//return nil
		},
	},
	{
		Name:        "config",
		Usage:       "get machine configuration",
		Description: "Print machine state information, e.g. `state get uuid` returns the machine uuid",
		Aliases:     []string{"c"},
		Action: func(c *cli.Context) error {
			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			fmt.Print(runtime)
			return err
		},
		Subcommands: []*cli.Command{
			{
				Name:        "show",
				Usage:       "Shows the machine configuration",
				Description: "Show the runtime configuration of the machine. It will scan the machine for all the configuration and will return the config file processed and found.",
				Aliases:     []string{"s"},
				Action: func(c *cli.Context) error {
					config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs)
					if err != nil {
						return err
					}

					configStr, err := config.String()
					if err != nil {
						return err
					}
					fmt.Printf("%s", configStr)
					return nil
				},
			},
			{
				Name:  "get",
				Usage: "Get specific data from the configuration",
				UsageText: `
Use it to retrieve configuration programmatically from the CLI:

$ kairos-agent config get k3s.enabled
true

or

$ kairos-agent config get k3s
enabled: true`,
				Description: "It allows to navigate the YAML config file by searching with 'yq' style keywords as `config get k3s` to retrieve the k3s config block",
				Aliases:     []string{"g"},
				Action: func(c *cli.Context) error {
					config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs, collector.StrictValidation(c.Bool("strict-validation")))
					if err != nil {
						return err
					}

					res, err := config.Query(c.Args().First())
					if err != nil {
						return err
					}
					fmt.Printf("%s", res)
					return nil
				},
			},
		},
	},
	{
		Name:        "state",
		Usage:       "get machine state",
		Description: "Print machine state information, e.g. `state get uuid` returns the machine uuid",
		Aliases:     []string{"s"},
		Action: func(c *cli.Context) error {
			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			fmt.Print(runtime)
			return err
		},
		Subcommands: []*cli.Command{
			{
				Name:        "apply",
				Usage:       "Applies a machine state",
				Description: "Applies machine configuration in runtimes",
				Aliases:     []string{"a"},
				Action: func(c *cli.Context) error {
					// TODO
					return nil
				},
			},
			{
				Name:        "get",
				Usage:       "get specific ",
				Description: "query state data",
				Aliases:     []string{"g"},
				Action: func(c *cli.Context) error {
					runtime, err := state.NewRuntime()
					if err != nil {
						return err
					}

					res, err := runtime.Query(c.Args().First())
					fmt.Print(res)
					return err
				},
			},
		},
	},
	{
		Name:        "render-template",
		Usage:       "Render a Go template",
		Description: "Render a Go template with machine state and config as data context",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "file",
				Aliases:  []string{"f"},
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {

			config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs, collector.StrictValidation(c.Bool("strict-validation")))
			if err != nil {
				return err
			}

			runtime, err := state.NewRuntime()
			if err != nil {
				return err
			}

			result, err := action.RenderTemplate(c.String("file"), config, runtime)
			if err != nil {
				return err
			}

			_, err = os.Stdout.Write(result)
			return err
		},
	},
	{
		Name: "interactive-install",
		Description: `
Starts kairos in interactive mode install.

It will ask prompt for several questions and perform an install depending on the providers available in the system.

See also https://kairos.io/installation/interactive_install/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be also started manually`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: "shell",
			},
			&sourceFlag,
		},
		Usage: "Starts interactive installation",
		Before: func(c *cli.Context) error {
			if err := validateSource(c.String("source")); err != nil {
				return err
			}

			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			source := c.String("source")

			return agent.InteractiveInstall(c.Bool("debug"), c.Bool("shell"), source)
		},
	},
	{
		Name:  "manual-install",
		Usage: "Starts the manual installation",
		Description: `
`,
		Aliases: []string{"m"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: "device",
			},
			&cli.BoolFlag{
				Name: "poweroff",
			},
			&cli.BoolFlag{
				Name: "reboot",
			},
			&sourceFlag,
		},
		Before: func(c *cli.Context) error {
			if err := validateSource(c.String("source")); err != nil {
				return err
			}

			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			if c.NArg() == 0 {
				return fmt.Errorf("expect one argument. the config file - if you don't have it, use the interactive-install")
			}
			config := c.Args().First()

			source := c.String("source")

			return agent.ManualInstall(config, source, c.String("device"), c.Bool("reboot"), c.Bool("poweroff"), c.Bool("strict-validation"))
		},
	},
	{
		Name:  "install",
		Usage: "Starts the kairos pairing installation",
		Description: `
Starts kairos in pairing mode.

It will print out a QR code which can be used with "kairos register" to send over a configuration and bootstraping a kairos node.

See also https://kairos.io/docs/installation/qrcode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can be started manually`,
		Aliases: []string{"i"},
		Before: func(c *cli.Context) error {
			if err := validateSource(c.String("source")); err != nil {
				return err
			}

			return checkRoot()
		},
		Flags: []cli.Flag{
			&sourceFlag,
		},
		Action: func(c *cli.Context) error {
			source := c.String("source")

			return agent.Install(source, configScanDir...)
		},
	},
	{
		Name:    "recovery",
		Aliases: []string{"r"},
		Action: func(c *cli.Context) error {
			return agent.Recovery()
		},
		Usage: "Starts kairos recovery mode",
		Description: `
Starts kairos recovery mode.

In recovery mode a QR code will be printed out on the screen which should be used in conjunction with "kairos bridge". Pass by the QR code as snapshot
to the bridge to connect over the machine which runs the "kairos recovery" command.

See also https://kairos.io/after_install/recovery_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},
	{
		Name: "reset",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "reboot",
				Usage: "Enable automated reboot after reset. Has precedence over any config in the system.",
			},
			&cli.BoolFlag{
				Name:  "unattended",
				Usage: "Do not wait for user input and provide ttys after reset. Also sets the fast mode (do not wait 60 seconds before reset)",
			},
		},
		Before: func(c *cli.Context) error {
			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			reboot := c.Bool("reboot")
			unattended := c.Bool("unattended")

			return agent.Reset(reboot, unattended, configScanDir...)
		},
		Usage: "Starts kairos reset mode",
		Description: `
Starts kairos reset mode, it will nuke completely the node data and restart fresh.
Attention ! this will delete any persistent data on the node. It is equivalent to re-init the node right after the installation.

In reset mode a the node will automatically reset

See also https://kairos.io/after_install/reset_mode/ for documentation.

This command is meant to be used from the boot GRUB menu, but can likely be used standalone`,
	},
	{
		Name: "validate",
		Action: func(c *cli.Context) error {
			config := c.Args().First()
			return schema.Validate(config)
		},
		Usage: "Validates a cloud config file",
		Description: `
The validate command expects a configuration file as its only argument. Local files and URLs are accepted.
		`,
	},
	{
		Name: "print-schema",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "version",
				Usage: "Print the current schema but with another version number, instead of using the agent version number.",
			},
		},
		Action: func(c *cli.Context) error {
			var version string
			if c.String("version") != "" {
				version = c.String("version")
			} else {
				version = common.VERSION
			}

			json, err := schema.JSONSchema(version)

			if err != nil {
				return err
			}

			fmt.Println(json)

			return nil
		},
		Usage:       "Print out Kairos' Cloud Configuration JSON Schema",
		Description: `Prints out Kairos' Cloud Configuration JSON Schema`,
	},
	{
		Name:        "run-stage",
		Description: "Run stage from cloud-init",
		Usage:       "Run stage from cloud-init",
		UsageText:   "run-stage STAGE",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "strict",
				Usage: "Enable strict mode. Fails and exits on stage errors",
			},
			&cli.StringSliceFlag{
				Name:  "cloud-init-paths",
				Usage: "Extra paths to add to the run stage",
			},
		},
		Before: func(c *cli.Context) error {
			if c.Args().Len() != 1 {
				cli.HelpPrinter(c.App.Writer, "Stage to run missing\n\n", c.Command)
				_ = cli.ShowSubcommandHelp(c)
				return fmt.Errorf("")
			}

			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			stage := c.Args().First()
			config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs)
			config.Strict = c.Bool("strict")

			if len(c.StringSlice("cloud-init-paths")) > 0 {
				config.CloudInitPaths = append(config.CloudInitPaths, c.StringSlice("cloud-init-paths")...)
			}
			if c.Bool("debug") {
				config.Logger.SetLevel(logrus.DebugLevel)
			}

			if err != nil {
				config.Logger.Errorf("Error reading config: %s\n", err)
			}
			return utils.RunStage(config, stage)
		},
	},
	{
		Name:        "pull-image",
		Description: "Pull remote image to local file",
		Usage:       "Pull remote image to local file",
		UsageText:   "pull-image [-l] IMAGE TARGET",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "platform",
				Usage: "Platform/arch to pull image from",
				Value: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
			},
		},
		Before: func(c *cli.Context) error {
			if c.Args().Len() != 2 {
				cli.HelpPrinter(c.App.Writer, "Either Image or target argument missing\n\n", c.Command)
				_ = cli.ShowSubcommandHelp(c)
				return fmt.Errorf("")
			}

			return checkRoot()
		},
		Action: func(c *cli.Context) error {
			image := c.Args().Get(0)
			destination, err := filepath.Abs(c.Args().Get(1))
			if err != nil {
				return fmt.Errorf("invalid path %s", destination)
			}
			config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs)
			if err != nil {
				return err
			}
			config.Logger.Infof("Starting download and extraction for image %s to %s\n", image, destination)
			e := v1.OCIImageExtractor{}
			if err = e.ExtractImage(image, destination, c.String("platform")); err != nil {
				return err
			}
			config.Logger.Infof("Image %s downloaded and extracted to %s correctly\n", image, destination)
			return nil
		},
	},
	{
		Name:        "version",
		Description: "Print kairos-agent version",
		Usage:       "Print kairos-agent version",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "long",
				Usage:   "Print long version info",
				Aliases: []string{"l"},
			},
		},
		Action: func(c *cli.Context) error {
			if c.Bool("long") {
				fmt.Printf("%+v\n", common.Get())
			} else {
				fmt.Println(common.VERSION)
			}
			return nil
		},
	},
	{
		Name:        "uki",
		Usage:       "UKI subcommands",
		Description: "UKI subcommands",
		// we could set the flag --source at this level so we could have the flag for all subcommands but that translates into an ugly command
		// in which you need to put the source flag before the subcommand, which is a mess. Just bad UX.
		// command level: kairos-agent uki --source oci:whatever install
		// subcommand level: kairos-agent uki install --source oci:whatever
		Subcommands: []*cli.Command{
			{
				Name:      "install",
				Usage:     "Install to disk",
				UsageText: "install [--device DEVICE]",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "source",
						Usage: "Source for install. Composed of `type:address`. Accepts `file:`,`dir:` or `oci:` for the type of source.\nFor example `file:/var/share/myimage.tar`, `dir:/tmp/extracted` or `oci:repo/image:tag`",
						Action: func(c *cli.Context, s string) error {
							return validateSource(s)
						},
					},
					&cli.StringFlag{
						Name: "device",
					},
				},
				Action: func(c *cli.Context) error {
					config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs)
					if err != nil {
						return err
					}
					// Load the spec from the config
					installSpec, err := agentConfig.ReadUkiInstallSpecFromConfig(config)
					if err != nil {
						return err
					}

					if c.String("device") != "" {
						installSpec.Target = c.String("device")
					}

					installAction := uki.NewInstallAction(config, installSpec)
					return installAction.Run()
				},
			},
			{
				Name: "upgrade",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "source",
						Usage: "Source for upgrade. Composed of `type:address`. Accepts `file:`,`dir:` or `oci:` for the type of source.\nFor example `file:/var/share/myimage.tar`, `dir:/tmp/extracted` or `oci:repo/image:tag`",
						Action: func(c *cli.Context, s string) error {
							return validateSource(s)
						},
					},
				},
				Before: func(c *cli.Context) error {
					return fmt.Errorf("not implemented")
				},
				Action: func(c *cli.Context) error {
					config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs, collector.StrictValidation(c.Bool("strict-validation")))
					if err != nil {
						return err
					}

					// Load the spec from the config
					upgradeSpec, err := agentConfig.ReadUkiUpgradeFromConfig(config)
					if err != nil {
						return err
					}

					upgradeAction := uki.NewUpgradeAction(config, upgradeSpec)
					return upgradeAction.Run()
				},
			},
			{
				Name: "reset",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "source",
						Usage: "Source for upgrade. Composed of `type:address`. Accepts `file:`,`dir:` or `oci:` for the type of source.\nFor example `file:/var/share/myimage.tar`, `dir:/tmp/extracted` or `oci:repo/image:tag`",
						Action: func(c *cli.Context, s string) error {
							return validateSource(s)
						},
					},
				},
				Before: func(c *cli.Context) error {
					return fmt.Errorf("not implemented")
				},
				Action: func(c *cli.Context) error {
					config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs, collector.StrictValidation(c.Bool("strict-validation")))
					if err != nil {
						return err
					}

					// Load the spec from the config
					resetSpec, err := agentConfig.ReadUkiResetSpecFromConfig(config)
					if err != nil {
						return err
					}

					resetAction := uki.NewResetAction(config, resetSpec)
					return resetAction.Run()
				},
			},
		},
	},
	{
		Name:        "utils",
		Usage:       "utils subcommands",
		Description: "utils subcommands",
		Subcommands: []*cli.Command{
			{
				Name:        "ABChange",
				Description: "Makes the passive partition the active one and vice-versa. Only for QCS6490 boards",
				Action: func(c *cli.Context) error {
					config, err := agentConfig.Scan(collector.Directories(configScanDir...), collector.NoLogs, collector.StrictValidation(c.Bool("strict-validation")))
					if err != nil {
						return err
					}

					err, out := boards.SetPassiveActive(config.Runner)
					if err != nil {
						config.Logger.Errorf("Failed to set passive partition as active: %s", err)
						config.Logger.Errorf("Failed setting passive partition as active(command output): %s", out)
						return err
					}
					return nil
				},
			},
		},
	},
}

func main() {
	bus.Manager.Initialize()

	app := &cli.App{
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "strict-validation",
				Usage:   "Fail instead of warn on validation errors.",
				EnvVars: []string{"STRICT_VALIDATIONS"},
			},
			&cli.BoolFlag{
				Name:    "debug",
				Usage:   "enable debug output",
				EnvVars: []string{"KAIROS_AGENT_DEBUG"},
			},
		},
		Name:    "kairos-agent",
		Version: common.VERSION,
		Authors: []*cli.Author{
			{
				Name: "Ettore Di Giacinto",
			},
		},
		Usage: "kairos agent start",
		Description: `
The kairos agent is a component to abstract away node ops, providing a common feature-set across kairos variants.
`,
		UsageText: ``,
		Copyright: "kairos authors",
		Before: func(c *cli.Context) error {
			// Set debug from here already, so it's loaded by the ReadConfigRun
			viper.Set("debug", c.Bool("debug"))
			return nil
		},
		Commands: cmds,
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func checkRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("this command requires root privileges")
	}

	return nil
}

func validateSource(source string) error {
	if source == "" {
		return nil
	}

	r, err := regexp.Compile(`^oci:|dir:|file:`)
	if err != nil {
		return err
	}
	if !r.MatchString(source) {
		return fmt.Errorf("source %s does not match any of oci:, dir: or file: ", source)
	}

	return nil
}
