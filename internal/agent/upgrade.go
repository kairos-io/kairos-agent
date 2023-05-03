package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/docker/docker/api/types"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos/v2/internal/bus"
	"github.com/kairos-io/kairos/v2/pkg/action"
	"github.com/kairos-io/kairos/v2/pkg/config"
	"github.com/kairos-io/kairos/v2/pkg/config/collector"
	"github.com/kairos-io/kairos/v2/pkg/elementalConfig"
	"github.com/kairos-io/kairos/v2/pkg/github"
	"github.com/kairos-io/kairos/v2/pkg/luet"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos/v2/pkg/utils"
	"github.com/mudler/go-pluggable"
	log "github.com/sirupsen/logrus"
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
	}

	return releases
}

func Upgrade(
	version, image string, force, debug, strictValidations bool, dirs []string, authUser string,
	authPass string, authServer string, authType string, registryToken string, identityToken string, preReleases bool,
) error {
	bus.Manager.Initialize()

	if version == "" && image == "" {
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

	discoveredImage := ""
	bus.Manager.Response(events.EventVersionImage, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		discoveredImage = r.Data
	})

	_, err := bus.Manager.Publish(events.EventVersionImage, &events.VersionImagePayload{
		Version: version,
	})
	if err != nil {
		return err
	}

	registry, err := utils.OSRelease("IMAGE_REPO")
	if err != nil {
		fmt.Printf("Cant find IMAGE_REPO key under /etc/os-release\n")
		return err
	}

	img := fmt.Sprintf("%s:%s", registry, version)
	if discoveredImage != "" {
		img = discoveredImage
	}
	if image != "" {
		img = image
	}

	if debug {
		fmt.Printf("Upgrading to image: '%s'\n", img)
	}

	c, err := config.Scan(collector.Directories(dirs...), collector.StrictValidation(strictValidations))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	// Load the upgrade Config from the system
	resetConfig, err := elementalConfig.ReadConfigRun("/etc/elemental")
	if err != nil {
		return err
	}
	if debug {
		resetConfig.Logger.SetLevel(log.DebugLevel)
	}

	// Generate an auth object
	auth := &types.AuthConfig{
		Username:      authUser,
		Password:      authPass,
		ServerAddress: authServer,
		Auth:          authType,
		IdentityToken: identityToken,
		RegistryToken: registryToken,
	}

	// Override the default luet to pass the auth
	// Remember to create the temp dir!
	tmpDir := elementalUtils.GetTempDir(&resetConfig.Config, "")
	l := luet.NewLuet(luet.WithLogger(resetConfig.Logger), luet.WithAuth(auth), luet.WithLuetTempDir(tmpDir))
	resetConfig.Luet = l

	// Generate the upgrade spec
	resetSpec, err := elementalConfig.ReadUpgradeSpec(resetConfig)
	if err != nil {
		return err
	}

	// Add the image source
	imgSource, err := v1.NewSrcFromURI(fmt.Sprintf("docker:%s", img))
	if err != nil {
		return err
	}
	resetSpec.Active.Source = imgSource

	// Sanitize (this is not required but good to do
	err = resetSpec.Sanitize()
	if err != nil {
		return err
	}

	upgradeAction := action.NewUpgradeAction(resetConfig, resetSpec)

	return upgradeAction.Run()
}
