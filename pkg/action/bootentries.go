package action

import (
	"fmt"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/elemental"

	"github.com/erikgeiser/promptkit/confirmation"
	"github.com/erikgeiser/promptkit/selection"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
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

// selectBootEntryGrub sets the default boot entry to the selected entry by modifying /oem/grubenv
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
	// Set the default entry to the selected entry on /oem/grubenv
	vars := map[string]string{
		"next_entry": entry,
	}
	err = utils.SetPersistentVariables("/oem/grubenv", vars, cfg.Fs)
	if err != nil {
		cfg.Logger.Errorf("could not set default boot entry: %s\n", err)
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
	efiPartition, err := partitions.GetEfiPartition(&cfg.Logger)
	if err != nil {
		return err
	}
	// Validate entry exists
	entries, err := listSystemdEntries(cfg, efiPartition)
	if err != nil {
		return err

	}
	originalEntries := entries
	// when there are only 4 entries, we can assume they are either cos (which will be replaced eventually), fallback, recovery or statereset
	if len(entries) == len(cnst.UkiDefaultMenuEntries()) {
		entries = cnst.UkiDefaultMenuEntries()
	}

	// Check that entry exists in the entries list
	err = entryInList(cfg, entry, entries)
	// we also accept "active" as a selection so we can migrate eventually from cos
	if err != nil && !strings.HasPrefix(entry, "active") {
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
	originalEntry := entry
	if !reflect.DeepEqual(originalEntries, entries) {
		// since we temporarily allow also active, here we need to first set entry to "cos" so it will match with the originalEntries
		if strings.HasPrefix(entry, "active") {
			entry = "cos"
		}
		for _, e := range originalEntries {
			if strings.HasPrefix(e, entry) {
				entry = e
			}
		}
	}
	bootFileName, err := bootNameToSystemdConf(entry)
	if err != nil {
		return err
	}
	bootName := fmt.Sprintf("%s.conf", bootFileName)
	// Set the default entry to the selected entry
	// This is the file name of the entry to be set as default, boot assesment doesnt seem to count as it uses the ID which is the config name
	systemdConf["default"] = bootName
	err = utils.SystemdBootConfWriter(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/loader.conf"), systemdConf)
	if err != nil {
		cfg.Logger.Errorf("could not write loader.conf: %s", err)
		return err
	}
	cfg.Logger.Infof("Default boot entry set to %s", originalEntry)
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

	// Remove the boot assesment from the name we show
	re := regexp.MustCompile(`\+\d+(-\d+)?$`)
	fileName = re.ReplaceAllString(fileName, "")

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

	if strings.HasPrefix(conf, "statereset") {
		bootName := "statereset"
		confName := strings.TrimPrefix(fileName, "statereset")

		if confName != "" {
			bootName = bootName + " " + strings.Trim(confName, "_")
		}

		return bootName, nil
	}

	return strings.ReplaceAll(fileName, "_", " "), nil
}

// bootNameToSystemdConf converts a boot name to a systemd-boot conf file name
// skips the .conf extension
func bootNameToSystemdConf(name string) (string, error) {
	differenciator := ""

	if strings.HasPrefix(name, "cos") {
		if name != "cos" {
			differenciator = "_" + strings.TrimPrefix(name, "cos ")
		}
		return "active" + differenciator, nil
	}

	if strings.HasPrefix(name, "active") {
		if name != "active" {
			differenciator = "_" + strings.TrimPrefix(name, "active ")
		}
		return "active" + differenciator, nil
	}

	if strings.HasPrefix(name, "fallback") {
		if name != "fallback" {
			differenciator = "_" + strings.TrimPrefix(name, "fallback ")
		}
		return "passive" + differenciator, nil
	}

	if strings.HasPrefix(name, "recovery") {
		if name != "recovery" {
			differenciator = "_" + strings.TrimPrefix(name, "recovery ")
		}
		return "recovery" + differenciator, nil
	}

	if strings.HasPrefix(name, "statereset") {
		if name != "statereset" {
			differenciator = "_" + strings.TrimPrefix(name, "statereset ")
		}
		return "statereset" + differenciator, nil
	}

	return strings.ReplaceAll(name, " ", "_"), nil
}

// listBootEntriesSystemd lists the boot entries available in the systemd-boot config files
// and prompts the user to select one
// then calls the underlying SelectBootEntry function to mange the entry writing and validation
func listBootEntriesSystemd(cfg *config.Config) error {
	var err error
	e := elemental.NewElemental(cfg)
	cleanup := utils.NewCleanStack()
	defer func() { err = cleanup.Cleanup(err) }()
	// Get EFI partition
	efiPartition, err := partitions.GetEfiPartition(&cfg.Logger)
	if err != nil {
		return err
	}
	// mount if not mounted
	if mounted, err := utils.IsMounted(cfg, efiPartition); !mounted && err == nil {
		if efiPartition.MountPoint == "" {
			efiPartition.MountPoint = cnst.EfiDir
		}
		err = e.MountPartition(efiPartition)
		if err != nil {
			cfg.Logger.Errorf("could not mount EFI partition: %s", err)
			return err
		}
		cleanup.Push(func() error { return e.UnmountPartition(efiPartition) })
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
func listSystemdEntries(cfg *config.Config, efiPartition *sdkTypes.Partition) ([]string, error) {
	var entries []string
	err := fsutils.WalkDirFs(cfg.Fs, filepath.Join(efiPartition.MountPoint, "loader/entries/"), func(path string, info os.DirEntry, err error) error {
		if err != nil {
			cfg.Logger.Errorf("Walking the dir %s", err.Error())
		}

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
	// Read grub config from 4 places
	// /etc/cos/grub.cfg
	// /run/initramfs/cos-state/grub/grub.cfg
	// /run/initramfs/cos-state/grub2/grub.cfg
	// /etc/kairos/branding/grubmenu.cfg
	// And grep the entries by checking the --id\s([A-z0-9]*)\s{ pattern
	// TODO: Check how to run this from livecd as it requires mounting state and grub?
	var entries []string
	for _, file := range []string{"/etc/cos/grub.cfg", "/run/initramfs/cos-state/grub/grub.cfg", "/etc/kairos/branding/grubmenu.cfg", "/run/initramfs/cos-state/grub2/grub.cfg"} {
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
