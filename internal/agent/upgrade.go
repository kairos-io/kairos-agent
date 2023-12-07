package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/kairos-io/kairos-sdk/versioneer"
)

func CurrentImage() (string, error) {
	artifact, err := versioneer.NewArtifactFromOSRelease()
	if err != nil {
		return "", fmt.Errorf("creating an Artifact from os-release: %w", err)
	}

	registryAndOrg, err := utils.OSRelease("REGISTRY_AND_ORG")
	if err != nil {
		return "", err
	}

	return artifact.ContainerName(registryAndOrg)
}

func ListReleases(includePrereleases bool) (versioneer.TagList, error) {
	var tagList versioneer.TagList
	var err error

	// TODO: Re-enable when the provider also consumes versioneer
	// TODO: Somehow pass includePrereleases to the provider?
	// bus.Manager.Response(events.EventAvailableReleases, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
	// 	if err := json.Unmarshal([]byte(r.Data), &tagList); err != nil {
	// 		fmt.Printf("warn: failed unmarshalling data: '%s'\n", err.Error())
	// 	}
	// })

	// if _, err := bus.Manager.Publish(events.EventAvailableReleases, events.EventPayload{}); err != nil {
	// 	fmt.Printf("warn: failed publishing event: '%s'\n", err.Error())
	// }

	// Sort before we filter
	// // We got the release list from the bus manager and we don't know if they are sorted, so sort them in reverse to get the latest first
	// // TODO: Should we sort? Maybe it's better to leave the sorting to the provider. Maybe there is custom logic baked in and
	// // we mess with it? Our provider can definitely do the same kind of sorting because it uses the same versioneer library.
	// sort.Sort(sort.Reverse(&tagList))

	tagList = versioneer.TagList{} // This will come from the provider-kairos above

	if len(tagList.Tags) == 0 {
		tagList, err = newerReleases()
		if err != nil {
			return tagList, err
		}

		if !includePrereleases {
			tagList = tagList.NoPrereleases()
		}
	}

	if len(tagList.Tags) == 0 {
		fmt.Println("No newer releases found")
	}

	return tagList, nil
}

func Upgrade(
	source string, force, strictValidations bool, dirs []string, preReleases, upgradeRecovery bool) error {
	bus.Manager.Initialize()

	upgradeSpec, c, err := generateUpgradeSpec(source, force, strictValidations, dirs, preReleases, upgradeRecovery)
	if err != nil {
		return err
	}

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

func newerReleases() (versioneer.TagList, error) {
	artifact, err := versioneer.NewArtifactFromOSRelease()
	if err != nil {
		return versioneer.TagList{}, err
	}

	registryAndOrg, err := utils.OSRelease("REGISTRY_AND_ORG")
	if err != nil {
		return versioneer.TagList{}, err
	}

	tagList, err := artifact.TagList(registryAndOrg)
	if err != nil {
		return tagList, err
	}

	return tagList.NewerAnyVersion().RSorted(), nil
}

// TODO: Remove the releavant hook from the provider-kairos too?
// determineUpgradeImage asks the provider plugin for an image or constructs
// it using version and data from /etc/os-release
// func determineUpgradeImage(version string) (*v1.ImageSource, error) {
// 	var img string
// 	bus.Manager.Response(events.EventVersionImage, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
// 		img = r.Data
// 	})

// 	_, err := bus.Manager.Publish(events.EventVersionImage, &events.VersionImagePayload{
// 		Version: version,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}

// 	if img != "" {
// 		return v1.NewSrcFromURI(img)
// 	}

// 	registry, err := utils.OSRelease("IMAGE_REPO")
// 	if err != nil {
// 		return nil, fmt.Errorf("can't find IMAGE_REPO key under /etc/os-release %w", err)
// 	}

// 	return v1.NewSrcFromURI(fmt.Sprintf("%s:%s", registry, version))
// }

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

func generateUpgradeSpec(sourceImageURL string, force, strictValidations bool, dirs []string, preReleases, upgradeRecovery bool) (*v1.UpgradeSpec, *config.Config, error) {
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

	return upgradeSpec, c, nil
}
