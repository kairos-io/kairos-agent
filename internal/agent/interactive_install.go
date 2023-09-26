package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"
	"github.com/kairos-io/kairos-agent/v2/internal/cmd"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	events "github.com/kairos-io/kairos-sdk/bus"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/unstructured"

	"github.com/erikgeiser/promptkit/textinput"
	"github.com/jaypipes/ghw"
	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/mudler/go-pluggable"
	"github.com/mudler/yip/pkg/schema"
	"github.com/pterm/pterm"
)

const (
	canBeEmpty = "Unset"
	yesNo      = "[y]es/[N]o"
)

func prompt(prompt, initialValue, placeHolder string, canBeEmpty, hidden bool) (string, error) {
	input := textinput.New(prompt)
	input.InitialValue = initialValue
	input.Placeholder = placeHolder
	if canBeEmpty {
		input.Validate = func(s string) error { return nil }
	}
	input.Hidden = hidden

	return input.RunPrompt()
}

func isYes(s string) bool {
	i := strings.ToLower(s)
	if i == "y" || i == "yes" {
		return true
	}
	return false
}

const (
	_ = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
)

func promptBool(p events.YAMLPrompt) (string, error) {
	def := "n"
	if p.Default != "" {
		def = p.Default
	}
	val, err := prompt(p.Prompt, def, yesNo, true, false)
	if err != nil {
		return "", err
	}
	if isYes(val) {
		val = "true"
	} else {
		val = "false"
	}

	return val, nil
}

func promptText(p events.YAMLPrompt) (string, error) {
	def := ""
	if p.Default != "" {
		def = p.Default
	}
	return prompt(p.Prompt, def, p.PlaceHolder, true, false)
}

func promptToUnstructured(p events.YAMLPrompt, unstructuredYAML map[string]interface{}) (map[string]interface{}, error) {
	var res string
	if p.AskFirst {
		ask, err := prompt(p.AskPrompt, "n", yesNo, true, false)
		if err == nil && !isYes(ask) {
			return unstructuredYAML, nil
		}
	}
	if p.Bool {
		val, err := promptBool(p)
		if err != nil {
			return unstructuredYAML, err
		}
		unstructuredYAML[p.YAMLSection] = val
		res = val
	} else {
		val, err := promptText(p)
		if err != nil {
			return unstructuredYAML, err
		}
		unstructuredYAML[p.YAMLSection] = val
		res = val
	}

	if res == "" && p.IfEmpty != "" {
		res = p.IfEmpty
		unstructuredYAML[p.YAMLSection] = res
	}
	return unstructuredYAML, nil
}

func detectDevice() string {
	preferedDevice := "/dev/sda"
	maxSize := float64(0)

	block, err := ghw.Block()
	if err == nil {
		for _, disk := range block.Disks {
			size := float64(disk.SizeBytes) / float64(GiB)
			if size > maxSize {
				maxSize = size
				preferedDevice = "/dev/" + disk.Name
			}
		}
	}
	return preferedDevice
}

func InteractiveInstall(debug, spawnShell bool, sourceImg string) error {
	var sshUsers []string
	bus.Manager.Initialize()

	cmd.PrintBranding(DefaultBanner)

	agentConfig, err := LoadConfig()
	if err != nil {
		return err
	}

	cmd.PrintText(agentConfig.Branding.InteractiveInstall, "Installation")

	disks := []string{}
	maxSize := float64(0)
	preferedDevice := "/dev/sda"

	block, err := ghw.Block()
	if err == nil {
		for _, disk := range block.Disks {
			size := float64(disk.SizeBytes) / float64(GiB)
			if size > maxSize {
				maxSize = size
				preferedDevice = "/dev/" + disk.Name
			}
			disks = append(disks, fmt.Sprintf("/dev/%s: %s (%.2f GiB) ", disk.Name, disk.Model, float64(disk.SizeBytes)/float64(GiB)))
		}
	}

	pterm.Info.Println("Available Disks:")
	for _, d := range disks {
		pterm.Info.Println(" " + d)
	}

	device, err := prompt("What's the target install device?", preferedDevice, "Cannot be empty", false, false)
	if err != nil {
		return err
	}

	userName, err := prompt("User to setup", "kairos", canBeEmpty, true, false)
	if err != nil {
		return err
	}

	userPassword, err := prompt("Password", "", canBeEmpty, true, true)
	if err != nil {
		return err
	}

	if userPassword == "" {
		userPassword = "!"
	}

	users, err := prompt("SSH access (rsakey, github/gitlab supported, comma-separated)", "github:someuser,github:someuser2", canBeEmpty, true, false)
	if err != nil {
		return err
	}

	// Cleanup the users if we selected the default values as they are not valid users
	if users == "github:someuser,github:someuser2" {
		users = ""
	}
	if users != "" {
		sshUsers = strings.Split(users, ",")
	}

	// Prompt the user by prompts defined by the provider
	r := []events.YAMLPrompt{}

	bus.Manager.Response(events.EventInteractiveInstall, func(p *pluggable.Plugin, resp *pluggable.EventResponse) {
		err := json.Unmarshal([]byte(resp.Data), &r)
		if err != nil {
			fmt.Println(err)
		}
	})

	_, err = bus.Manager.Publish(events.EventInteractiveInstall, events.EventPayload{})
	if err != nil {
		return err
	}

	unstructuredYAML := map[string]interface{}{}
	for _, p := range r {
		unstructuredYAML, err = promptToUnstructured(p, unstructuredYAML)
		if err != nil {
			return err
		}
	}

	result, err := unstructured.ToYAMLMap(unstructuredYAML)
	if err != nil {
		return err
	}

	allGood, err := prompt("Are settings ok?", "n", yesNo, true, false)
	if err != nil {
		return err
	}

	if !isYes(allGood) {
		return InteractiveInstall(debug, spawnShell, sourceImg)
	}

	usersToSet := map[string]schema.User{}

	stage := config.NetworkStage.String()

	if userName != "" {
		user := schema.User{
			Name:              userName,
			PasswordHash:      userPassword,
			Groups:            []string{"admin"},
			SSHAuthorizedKeys: sshUsers,
		}

		// If we got no ssh keys, we don't need network, do the user as soon as possible
		if len(sshUsers) == 0 {
			stage = config.InitramfsStage.String()
		}

		usersToSet = map[string]schema.User{
			userName: user,
		}
	}

	cloudConfig := schema.YipConfig{Name: "Config generated by the installer",
		Stages: map[string][]schema.Stage{stage: {
			{
				Users: usersToSet,
			},
		}}}

	// This is temporal to generate a valid cc file, no need to properly initialize everything
	cc := &config.Config{
		Install: &config.Install{
			Device: device,
		},
	}
	// Merge all yamls into one
	dat, err := config.MergeYAML(cloudConfig, cc, result)
	if err != nil {
		return err
	}

	finalCloudConfig := config.AddHeader("#cloud-config", string(dat))

	// Store it in a temp file and load it with the collector to have a standard way of loading across all methods
	tmpdir, err := os.MkdirTemp("", "kairos-install-")
	if err == nil {
		err = os.WriteFile(filepath.Join(tmpdir, "kairos-event-install-data.yaml"), []byte(finalCloudConfig), os.ModePerm)
		if err != nil {
			fmt.Printf("could not write event cloud init: %s\n", err.Error())
		}
		// override cc with our new config object from the scan, so it's updated for the RunInstall function
		// TODO: Alternative solution: pass a reader here (the new feature) and add the image source
		cc, _ = config.Scan(collector.Directories(tmpdir), collector.MergeBootLine, collector.NoLogs)
	}

	pterm.Info.Println("Starting installation")
	// Generate final config
	ccString, _ := cc.String()
	pterm.Info.Println(ccString)

	err = RunInstall(cc, sourceImg)
	if err != nil {
		pterm.Error.Println(err.Error())
	}

	if spawnShell {
		return utils.Shell().Run()
	}
	return err
}
