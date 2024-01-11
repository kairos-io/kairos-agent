package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/mudler/go-pluggable"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	config "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/uki"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
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

func ListAllReleases(includePrereleases bool) ([]string, error) {
	var err error

	tagList, err := allReleases()
	if err != nil {
		return []string{}, err
	}

	if !includePrereleases {
		tagList = tagList.NoPrereleases()
	}

	return tagList.FullImages()
}

func ListNewerReleases(includePrereleases bool) ([]string, error) {
	var err error

	tagList, err := newerReleases()
	if err != nil {
		return []string{}, err
	}

	if !includePrereleases {
		tagList = tagList.NoPrereleases()
	}

	return tagList.FullImages()
}

func Upgrade(
	source string, force, strictValidations bool, dirs []string, preReleases, upgradeRecovery bool) error {
	bus.Manager.Initialize()

	if internalutils.UkiBootMode() == internalutils.UkiRemovableMedia {
		return upgradeUki(source, dirs, strictValidations)
	} else {
		return upgrade(source, force, strictValidations, dirs, preReleases, upgradeRecovery)
	}
}

func upgrade(source string, force, strictValidations bool, dirs []string, preReleases, upgradeRecovery bool) error {
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

func allReleases() (versioneer.TagList, error) {
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

	return tagList.OtherAnyVersion().RSorted(), nil
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
	//fmt.Printf("tagList.OtherAnyVersion() = %#v\n", tagList.OtherAnyVersion().Tags)
	//fmt.Printf("tagList.Images() = %#v\n", tagList.Images().Tags)
	// fmt.Println("Tags")
	// tagList.NewerAnyVersion().Print()
	// fmt.Println("---------------------------")

	return tagList.NewerAnyVersion().RSorted(), nil
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

func getReleasesFromProvider(includePrereleases bool) ([]string, error) {
	var result []string
	bus.Manager.Response(events.EventAvailableReleases, func(p *pluggable.Plugin, r *pluggable.EventResponse) {
		if r.Data == "" {
			return
		}
		if err := json.Unmarshal([]byte(r.Data), &result); err != nil {
			fmt.Printf("warn: failed unmarshalling data: '%s'\n", err.Error())
		}
	})

	configYAML := "IncludePreReleases: true"
	_, err := bus.Manager.Publish(events.EventAvailableReleases, events.EventPayload{Config: configYAML})
	if err != nil {
		return result, fmt.Errorf("failed publishing event: %w", err)
	}

	return result, nil
}

func upgradeUki(source string, dirs []string, strictValidations bool) error {
	cliConf, err := generateUpgradeConfForCLIArgs(source, false)
	if err != nil {
		return err
	}

	c, err := config.Scan(collector.Directories(dirs...),
		collector.Readers(strings.NewReader(cliConf)),
		collector.StrictValidation(strictValidations))
	if err != nil {
		return err
	}

	utils.SetEnv(c.Env)

	// Load the upgrade Config from the system
	upgradeSpec, err := config.ReadUkiUpgradeFromConfig(c)
	if err != nil {
		return err
	}

	err = upgradeSpec.Sanitize()
	if err != nil {
		return err
	}

	upgradeAction := uki.NewUpgradeAction(c, upgradeSpec)

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
