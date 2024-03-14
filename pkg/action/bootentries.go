package action

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"

	"github.com/erikgeiser/promptkit/confirmation"
	"github.com/erikgeiser/promptkit/selection"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
)

// SelectBootEntry sets the default boot entry to the selected entry
// This is the entrypoint for the bootentry action with --select flag
// also other actions can call this function to set the default boot entry
func SelectBootEntry(cfg *config.Config, entry string) error {
	if utils.IsUkiWithFs(cfg.Fs) {
		return selectBootEntrySystemd(cfg, entry)
	} else {
		return selectBootEntryGrub(cfg, entry)
	}
}

// ListBootEntries lists the boot entries available in the system and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func ListBootEntries(cfg *config.Config) error {
	if utils.IsUkiWithFs(cfg.Fs) {
		return listBootEntriesSystemd(cfg)
	} else {
		return listBootEntriesGrub(cfg)
	}
}

// selectBootEntryGrub sets the default boot entry to the selected entry via `grub2-editenv /oem/grubenv set next_entry=entry`
// also validates that the entry exists in our list of entries
func selectBootEntryGrub(cfg *config.Config, entry string) error {
	// Validate if entry exists
	entries, err := listGrubEntries(cfg)
	if err != nil {
		return err
	}
	// Check that entry exists in the entries list
	err = entryInList(cfg, entry, entries)
	if err != nil {
		return err
	}
	cfg.Logger.Infof("Setting default boot entry to %s", entry)
	// Set the default entry to the selected entry via `grub2-editenv /oem/grubenv set next_entry=statereset`
	out, err := cfg.Runner.Run("grub2-editenv", "/oem/grubenv", "set", fmt.Sprintf("next_entry=%s", entry))
	if err != nil {
		cfg.Logger.Errorf("could not set default boot entry: %s\noutput: %s", err, out)
		return err
	}
	cfg.Logger.Infof("Default boot entry set to %s", entry)
	return err
}

// selectBootEntrySystemd sets the default boot entry to the selected entry via modifying the loader.conf file
// also validates that the entry exists in our list of entries
func selectBootEntrySystemd(cfg *config.Config, entry string) error {
	cfg.Logger.Infof("Setting default boot entry to %s", entry)

	// Get EFI partition
	efiPartition, err := partitions.GetEfiPartition()
	if err != nil {
		return err
	}
	// Validate entry exists
	entries, err := listSystemdEntries(cfg, efiPartition)
	if err != nil {
		return err

	}
	originalEntries := entries
	// when there are only 3 entries, we can assume they are active, passive and recovery
	if len(entries) == 3 {
		entries = []string{"cos", "fallback", "recovery"}
	}
	// when there are more than 3 entries, then we need to also extract the part between the first _ and the .conf in order to distinguish the entries

	// Check that entry exists in the entries list
	err = entryInList(cfg, entry, entries)
	if err != nil {
		return err
	}

	// Mount it RW
	err = cfg.Syscall.Mount("", efiPartition.MountPoint, "", syscall.MS_REMOUNT, "")
	if err != nil {
		cfg.Logger.Errorf("could not remount EFI partition: %s", err)
		return err
	}
	// Remount it RO when finished
	defer func(source string, target string, fstype string, flags uintptr, data string) {
		err = cfg.Syscall.Mount(source, target, fstype, flags, data)
		if err != nil {
			cfg.Logger.Errorf("could not remount EFI partition as RO: %s", err)
		}
	}("", efiPartition.MountPoint, "", syscall.MS_REMOUNT|syscall.MS_RDONLY, "")

	systemdConf, err := utils.SystemdBootConfReader(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/loader.conf"))
	if err != nil {
		cfg.Logger.Errorf("could not read loader.conf: %s", err)
		return err
	}
	if !reflect.DeepEqual(originalEntries, entries) {
		for _, e := range originalEntries {
			if strings.HasPrefix(e, entry) {
				entry = e
			}
		}
	}
	bootName, err := bootNameToSystemdConf(entry)
	if err != nil {
		return err
	}
	// Set the default entry to the selected entry
	systemdConf["default"] = bootName
	err = utils.SystemdBootConfWriter(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/loader.conf"), systemdConf)
	if err != nil {
		cfg.Logger.Errorf("could not write loader.conf: %s", err)
		return err
	}
	cfg.Logger.Infof("Default boot entry set to %s", entry)
	return err
}

// listBootEntriesGrub lists the boot entries available in the grub config files
// and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func listBootEntriesGrub(cfg *config.Config) error {
	entries, err := listGrubEntries(cfg)
	if err != nil {
		return err
	}
	// create a selector
	selector := selection.New("Select Next Boot Entry", entries)
	selector.Filter = nil        // Remove the filter
	selector.ResultTemplate = `` // Do not print the result as we are asking for confirmation afterwards
	selected, _ := selector.RunPrompt()
	c := confirmation.New("Are you sure you want to change the boot entry to "+selected, confirmation.Yes)
	c.ResultTemplate = ``
	confirm, err := c.RunPrompt()
	if confirm {
		return SelectBootEntry(cfg, selected)
	}
	return err
}

func systemdConfToBootName(conf string) (string, error) {
	if !strings.HasSuffix(conf, ".conf") {
		return "", fmt.Errorf("unknown systemd-boot conf: %s", conf)
	}

	fileName := strings.TrimSuffix(conf, ".conf")

	if strings.HasPrefix(fileName, "active") {
		bootName := "cos"
		confName := strings.TrimPrefix(fileName, "active")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	if strings.HasPrefix(fileName, "passive") {
		bootName := "fallback"
		confName := strings.TrimPrefix(fileName, "passive")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	if strings.HasPrefix(conf, "recovery") {
		bootName := "recovery"
		confName := strings.TrimPrefix(fileName, "recovery")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	return "", fmt.Errorf("unknown systemd-boot conf: %s", conf)
}

func bootNameToSystemdConf(name string) (string, error) {
	differenciator := ""

	if strings.HasPrefix(name, "cos") {
		if name != "cos" {
			differenciator = "_" + strings.TrimPrefix(name, "cos ")
		}
		return "active" + differenciator + ".conf", nil
	}

	if strings.HasPrefix(name, "fallback") {
		if name != "fallback" {
			differenciator = "_" + strings.TrimPrefix(name, "fallback ")
		}
		return "passive" + differenciator + ".conf", nil
	}

	if strings.HasPrefix(name, "recovery") {
		if name != "recovery" {
			differenciator = "_" + strings.TrimPrefix(name, "recovery ")
		}
		return "recovery" + differenciator + ".conf", nil
	}

	return "", fmt.Errorf("unknown boot name: %s", name)
}

// listBootEntriesSystemd lists the boot entries available in the systemd-boot config files
// and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func listBootEntriesSystemd(cfg *config.Config) error {
	// Get EFI partition
	efiPartition, err := partitions.GetEfiPartition()
	if err != nil {
		return err
	}
	// Get default entry from loader.conf
	loaderConf, err := utils.SystemdBootConfReader(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/loader.conf"))
	if err != nil {
		return err
	}

	entries, err := listSystemdEntries(cfg, efiPartition)
	if err != nil {
		return err
	}

	currentlySelected, err := systemdConfToBootName(loaderConf["default"])

	// create a selector
	selector := selection.New(fmt.Sprintf("Select Boot Entry (current entry: %s)", currentlySelected), entries)
	selector.Filter = nil        // Remove the filter
	selector.ResultTemplate = `` // Do not print the result as we are asking for confirmation afterwards
	selected, _ := selector.RunPrompt()
	c := confirmation.New("Are you sure you want to change the boot entry to "+selected, confirmation.Yes)
	c.ResultTemplate = ``
	confirm, err := c.RunPrompt()
	if err != nil {
		return err
	}

	if confirm {
		return SelectBootEntry(cfg, selected)
	}
	return err
}

// ListSystemdEntries reads the systemd-boot entries and returns a list of entries found
func listSystemdEntries(cfg *config.Config, efiPartition *v1.Partition) ([]string, error) {
	var entries []string
	err := fsutils.WalkDirFs(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/entries/"), func(path string, info os.DirEntry, err error) error {
		cfg.Logger.Debugf("Checking file %s", path)
		if info == nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(info.Name()) != ".conf" {
			return nil
		}
		entry, err := systemdConfToBootName(info.Name())
		if err != nil {
			return err
		}

		entries = append(entries, entry)
		return nil
	})
	return entries, err
}

// listGrubEntries reads the grub config files and returns a list of entries found
func listGrubEntries(cfg *config.Config) ([]string, error) {
	// Read grub config from 3 places
	// /etc/cos/grub.cfg
	// /run/initramfs/cos-state/grub/grub.cfg
	// /etc/kairos/branding/grubmenu.cfg
	// And grep the entries by checking the --id\s([A-z0-9]*)\s{ pattern
	var entries []string
	for _, file := range []string{"/etc/cos/grub.cfg", "/run/initramfs/cos-state/grub/grub.cfg", "/etc/kairos/branding/grubmenu.cfg"} {
		f, err := cfg.Fs.ReadFile(file)
		if err != nil {
			cfg.Logger.Errorf("could not read file %s: %s", file, err)
			continue
		}
		re, _ := regexp.Compile(`--id\s([A-z0-9]*)\s{`)
		matches := re.FindAllStringSubmatch(string(f), -1)
		for _, match := range matches {
			entries = append(entries, match[1])
		}
	}
	entries = uniqueStringArray(entries)
	return entries, nil
}

// I lost count on how many places I had to implement this function
// Is that difficult to provide a simple []string.Unique() method from the standard lib????
// Or at least a Set type?
func uniqueStringArray(arr []string) []string {
	unique := make(map[string]bool)
	var result []string
	for _, s := range arr {
		if !unique[s] {
			unique[s] = true
			result = append(result, s)
		}
	}
	return result
}

// Another one. Seriously there is nothing to check if something is in a list?
func entryInList(cfg *config.Config, entry string, list []string) error {
	// Check that entry exists in the entries list
	for _, e := range list {
		if e == entry {
			return nil
		}
	}
	cfg.Logger.Errorf("entry %s does not exist", entry)
	cfg.Logger.Debugf("entries: %v", list)
	return fmt.Errorf("entry %s does not exist", entry)
}
