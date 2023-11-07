package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"sort"
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"

	"github.com/Masterminds/semver/v3"
	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/github"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	boardutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/boards"
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

	upgradeSpec, c, err := generateUpgradeSpec(version, source, force, strictValidations, dirs, preReleases, upgradeRecovery)
	if err != nil {
		return err
	}

	err = upgradeSpec.Sanitize()
	if err != nil {
		return err
	}

	if boardutils.GetAndroidBoardModel() == constants.QCS6490 {
		if err = action.NewUpgradeQCS6490Action(c, upgradeSpec).Run(); err != nil {
			return err
		}
	} else {
		if err = action.NewUpgradeAction(c, upgradeSpec).Run(); err != nil {
			return err
		}
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
func determineUpgradeImage(version string) (*v1.ImageSource, error) {
	var img string
	bus.Manager.Response(events.EventVersionImage, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		img = r.Data
	})

	_, err := bus.Manager.Publish(events.EventVersionImage, &events.VersionImagePayload{
		Version: version,
	})
	if err != nil {
		return nil, err
	}

	if img != "" {
		return v1.NewSrcFromURI(img)
	}

	registry, err := utils.OSRelease("IMAGE_REPO")
	if err != nil {
		return nil, fmt.Errorf("can't find IMAGE_REPO key under /etc/os-release %w", err)
	}

	return v1.NewSrcFromURI(fmt.Sprintf("%s:%s", registry, version))
}

// generateUpgradeConfForCLIArgs creates a kairos configuration for `--source` and `--recovery`
// command line arguments. It will be added to the rest of the configurations.
func generateUpgradeConfForCLIArgs(source string, upgradeRecovery bool) (string, error) {
	upgrade := map[string](map[string]interface{}){
		"upgrade": {},
	}

	if upgradeRecovery {
		upgrade["upgrade"]["recovery"] = "true"
	}

	// Set uri both for active and recovery because we don't know what we are
	// actually upgrading. The "upgradeRecovery" is just the command line argument.
	// The user might have set it to "true" in the kairos config. Since we don't
	// have access to that yet, we just set both uri values which shouldn't matter
	// anyway, the right one will be used later in the process.
	if source != "" {
		upgrade["upgrade"]["recovery-system"] = map[string]string{
			"uri": source,
		}
		upgrade["upgrade"]["system"] = map[string]string{
			"uri": source,
		}
	}

	d, err := json.Marshal(upgrade)

	return string(d), err
}

func handleEmptySource(spec *v1.UpgradeSpec, version string, preReleases, force bool) error {
	var err error
	if spec.RecoveryUpgrade {
		if spec.Recovery.Source.IsEmpty() {
			spec.Recovery.Source, err = getLatestOrConstructSource(version, preReleases, force)
		}
	} else {
		if spec.Active.Source.IsEmpty() {
			spec.Active.Source, err = getLatestOrConstructSource(version, preReleases, force)
		}
	}

	return err
}

func getLatestOrConstructSource(version string, preReleases, force bool) (*v1.ImageSource, error) {
	var err error
	if version == "" {
		version, err = findLatestVersion(preReleases, force)
		if err != nil {
			return nil, err
		}
	}

	return determineUpgradeImage(version)
}

func findLatestVersion(preReleases, force bool) (string, error) {
	fmt.Println("Searching for releases")
	if preReleases {
		fmt.Println("Including pre-releases")
	}
	releases := ListReleases(preReleases)

	if len(releases) == 0 {
		return "", fmt.Errorf("no releases found")
	}

	// Using Original here because the parsing removes the v as its a semver. But it stores the original full version there
	version := releases[0].Original()

	if utils.Version() == version && !force {
		return "", fmt.Errorf("version %s already installed. use --force to force upgrade", version)
	}

	msg := fmt.Sprintf("Latest release is %s\nAre you sure you want to upgrade to this release? (y/n)", version)
	reply, err := promptBool(events.YAMLPrompt{Prompt: msg, Default: "y"})
	if err != nil {
		return "", err
	}
	if reply == "false" {
		return "", fmt.Errorf("cancelled by the user")
	}

	return version, nil
}

func generateUpgradeSpec(version, sourceImageURL string, force, strictValidations bool, dirs []string, preReleases, upgradeRecovery bool) (*v1.UpgradeSpec, *config.Config, error) {
	cliConf, err := generateUpgradeConfForCLIArgs(sourceImageURL, upgradeRecovery)
	if err != nil {
		return nil, nil, err
	}

	c, err := config.Scan(collector.Directories(dirs...),
		collector.Readers(strings.NewReader(cliConf)),
		collector.StrictValidation(strictValidations))
	if err != nil {
		return nil, nil, err
	}

	utils.SetEnv(c.Env)

	// Load the upgrade Config from the system
	upgradeSpec, err := config.ReadUpgradeSpecFromConfig(c)
	if err != nil {
		return nil, nil, err
	}

	err = handleEmptySource(upgradeSpec, version, preReleases, force)
	if err != nil {
		return nil, nil, err
	}

	return upgradeSpec, c, nil
}
