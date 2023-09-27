package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"

	"github.com/Masterminds/semver/v3"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/github"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/mudler/go-pluggable"
)

func ListReleases(includePrereleases bool) semver.Collection {
	var releases semver.Collection

	bus.Manager.Response(events.EventAvailableReleases, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		if err := json.Unmarshal([]byte(r.Data), &releases); err != nil {
			fmt.Printf("warn: failed unmarshalling data: '%s'\n", err.Error())
		}
	})

	if _, err := bus.Manager.Publish(events.EventAvailableReleases, events.EventPayload{}); err != nil {
		fmt.Printf("warn: failed publishing event: '%s'\n", err.Error())
	}

	if len(releases) == 0 {
		githubRepo, err := utils.OSRelease("GITHUB_REPO")
		if err != nil {
			return releases
		}
		fmt.Println("Searching for releases")
		if includePrereleases {
			fmt.Println("Including pre-releases")
		}
		releases, _ = github.FindReleases(context.Background(), "", githubRepo, includePrereleases)
	} else {
		// We got the release list from the bus manager and we don't know if they are sorted, so sort them in reverse to get the latest first
		sort.Sort(sort.Reverse(releases))
	}
	return releases
}

func Upgrade(
	version, source string, force, strictValidations bool, dirs []string, preReleases, upgradeRecovery bool) error {
	bus.Manager.Initialize()

	// TODO: Before we check for empy source,
	// shouldn't we read the `upgrade:` block from the config and check if something
	// is defined there?

	if version == "" && source == "" {
		fmt.Println("Searching for releases")
		if preReleases {
			fmt.Println("Including pre-releases")
		}
		releases := ListReleases(preReleases)

		if len(releases) == 0 {
			return fmt.Errorf("no releases found")
		}

		// Using Original here because the parsing removes the v as its a semver. But it stores the original full version there
		version = releases[0].Original()

		if utils.Version() == version && !force {
			fmt.Printf("version %s already installed. use --force to force upgrade\n", version)
			return nil
		}
		msg := fmt.Sprintf("Latest release is %s\nAre you sure you want to upgrade to this release? (y/n)", version)
		reply, err := promptBool(events.YAMLPrompt{Prompt: msg, Default: "y"})
		if err != nil {
			return err
		}
		if reply == "false" {
			return nil
		}
	}

	img := source
	var err error
	if img == "" {
		img, err = determineUpgradeImage(version)
		if err != nil {
			fmt.Println(err.Error())
			return err
		}
	}

	upgradeConf := generateUpgradeConf(img, upgradeRecovery)

	c, err := config.Scan(collector.Directories(dirs...),
		collector.Readers(strings.NewReader(upgradeConf)),
		collector.StrictValidation(strictValidations))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	// Load the upgrade Config from the system
	upgradeSpec, err := config.ReadUpgradeSpecFromConfig(c)
	if err != nil {
		return err
	}

	// Sanitize
	err = upgradeSpec.Sanitize()
	if err != nil {
		return err
	}

	upgradeAction := action.NewUpgradeAction(c, upgradeSpec)

	err = upgradeAction.Run()
	if err != nil {
		return err
	}

	if upgradeSpec.Reboot {
		utils.Reboot()
	}

	if upgradeSpec.PowerOff {
		utils.PowerOFF()
	}

	return hook.Run(*c, upgradeSpec, hook.AfterUpgrade...)
}

// determineUpgradeImage asks the provider plugin for an image or constructs
// it using version and data from /etc/os-release
func determineUpgradeImage(version string) (string, error) {
	var img string
	bus.Manager.Response(events.EventVersionImage, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		img = r.Data
	})

	_, err := bus.Manager.Publish(events.EventVersionImage, &events.VersionImagePayload{
		Version: version,
	})
	if err != nil {
		return "", err
	}

	if img != "" {
		return img, nil
	}

	registry, err := utils.OSRelease("IMAGE_REPO")
	if err != nil {
		return "", fmt.Errorf("can't find IMAGE_REPO key under /etc/os-release %w", err)
	}

	return fmt.Sprintf("%s:%s", registry, version), nil
}

// generateUpgradeConf creates a kairos configuration for upgrade to be
// added to the rest of the configurations.
func generateUpgradeConf(source string, upgradeRecovery bool) string {
	conf := ""

	if source == "" {
		return conf
	}

	// TODO: Do the same for recovery upgrade
	conf = fmt.Sprintf(`
upgrade:
	recovery-system:
		uri: %s`, source)

	// source := viper.GetString("upgradeSource")
	// recoveryUpgrade := viper.GetBool("upgradeRecovery")
	// if source != "" {
	// 	imgSource, err := v1.NewSrcFromURI(source)
	// 	// TODO: Don't hide the error here!
	// 	if err == nil {
	// 		if recoveryUpgrade {
	// 			spec.RecoveryUpgrade = recoveryUpgrade
	// 			spec.Recovery.Source = imgSource
	// 		} else {
	// 			spec.Active.Source = imgSource
	// 		}
	// 		size, err := GetSourceSize(cfg, imgSource)
	// 		if err != nil {
	// 			cfg.Logger.Warnf("Failed to infer size for images: %s", err.Error())
	// 		} else {
	// 			cfg.Logger.Infof("Setting image size to %dMb", size)
	// 			// On upgrade only the active or recovery will be upgraded, so we dont need to override passive
	// 			if recoveryUpgrade {
	// 				spec.Recovery.Size = uint(size)
	// 			} else {
	// 				spec.Active.Size = uint(size)
	// 			}
	// 		}
	// 	}
	//}

	return conf
}
