package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/elementalConfig"
	"github.com/kairos-io/kairos-agent/v2/pkg/github"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
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
	version, source string, force, strictValidations bool, dirs []string, preReleases bool) error {
	bus.Manager.Initialize()

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

	c, err := config.Scan(collector.Directories(dirs...), collector.StrictValidation(strictValidations))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	// Load the upgrade Config from the system
	upgradeConfig, upgradeSpec, err := elementalConfig.ReadUpgradeConfigFromAgentConfig(c)
	if err != nil {
		return err
	}

	// Add the image source
	imgSource, err := v1.NewSrcFromURI(img)
	if err != nil {
		return err
	}
	upgradeSpec.Active.Source = imgSource

	// Sanitize
	err = upgradeSpec.Sanitize()
	if err != nil {
		return err
	}

	upgradeAction := action.NewUpgradeAction(upgradeConfig, upgradeSpec)

	return upgradeAction.Run()
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
