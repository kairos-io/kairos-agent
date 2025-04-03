package action

import (
	"fmt"
	"github.com/distribution/reference"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	http2 "github.com/kairos-io/kairos-agent/v2/pkg/http"
	"github.com/kairos-io/kairos-sdk/types"
	"github.com/kairos-io/kairos-sdk/utils"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
)

// Implementation details for not trusted boot
// sysext are stored under
// /var/lib/kairos/extensions/
// we link them to /var/lib/kairos/extensions/{active,passive} depending on where we want it to be enabled
// Immucore on boot after mounting the persistent dir, will check those dirs\
// it will then create the proper links to them under /run/extensions
// This means they are enabled on boot and they are ephemeral, nothing is left behind in the actual sysext dirs
// This prevents us from having to clean up in different dirs, we can just do cleaning in our dirs (remove links)
// and on reboot they will not be enabled on boot
// So all the actions (list, upgrade, download, remove) will be done on the persistent dir
// And on boot we dinamycally link and enable them based on the boot type (active,passive) via immucore

// Trusted boot works very differently
// In there, we copy the sysext into the EFI dir under the entry /EFI/kairos/$EFI_FILE.efi.extra.d/EXTENSION_FILE
// That makes it so systemd-boot will see it, measure it and pass it to the initramfs under the /.extra/sysext dir
// That means two things
// We cannot soft link it as EFI partition is FAT
// To add/remove/update we need to deal with the actual files
// Currently we are NOT using the measuring of the sysextensions so maybe we could move into a unified system with the same behaviour as above?
// The sysextensions in Trusted boot are SIGNED so the measurement is not that important and immucore will refuse to move
// them into /run/extensions if the signature doesnt match, so the measurements are not that important currently and
// maybe they are not useful if sysext is the way of upgrading your system

// So potentially we have a unified system where we have the same behaviour for both, but that measn that
// anything provided by sysext will NOT be available until the initramfs stage where we enable the sysext service
// we can potentially move the sysext service as early as possible once we mount the persistent dir

// Extensions are provided in the following formats:
// - .raw : image based extension
// - any directory : directory based extension

const (
	sysextDir        = "/var/lib/kairos/extensions/"
	sysextDirActive  = "/var/lib/kairos/extensions/active"
	sysextDirPassive = "/var/lib/kairos/extensions/passive"
	sysextEnabledDir = "/run/extensions/"
)

// SysExtension represents a system extension
type SysExtension struct {
	Name     string
	Location string
}

func (s *SysExtension) String() string {
	return s.Name
}

// ListSystemExtensions lists the system extensions in the given directory
// If none is passed then it shows the generic ones
func ListSystemExtensions(cfg *config.Config, bootState string) ([]SysExtension, error) {
	switch bootState {
	case "active":
		cfg.Logger.Debug("Listing active system extensions")
		return getDirExtensions(cfg, sysextDirActive)
	case "passive":
		cfg.Logger.Debug("Listing passive system extensions")
		return getDirExtensions(cfg, sysextDirPassive)
	default:
		cfg.Logger.Debug("Listing all system extensions (Enabled or not)")
		return getDirExtensions(cfg, sysextDir)
	}
}

// getDirExtensions lists the system extensions in the given directory
func getDirExtensions(cfg *config.Config, dir string) ([]SysExtension, error) {
	var out []SysExtension
	// get all the extensions in the sysextDir
	entries, err := os.ReadDir(dir)
	// We don't care if the dir does not exist, we just return an empty list
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".raw" {
			out = append(out, SysExtension{Name: entry.Name(), Location: filepath.Join(dir, entry.Name())})
		}
	}
	return out, nil
}

// GetSystemExtension returns the system extension for a given name
func GetSystemExtension(cfg *config.Config, name, bootState string) (SysExtension, error) {
	// Get a list of all installed system extensions
	installed, err := ListSystemExtensions(cfg, bootState)
	if err != nil {
		return SysExtension{}, err
	}
	// Check if the extension is installed
	// regex against the name
	re, _ := regexp.Compile(name)
	for _, ext := range installed {
		if re.MatchString(ext.Name) {
			return ext, nil
		}
	}
	// If not, return an error
	return SysExtension{}, fmt.Errorf("system extension %s not found", name)
}

// EnableSystemExtension enables a system extension that is already in the system for a given bootstate
// It creates a symlink to the extension in the target dir according to the bootstate given
// It will create the target dir if it does not exist
// It will check if the extension is already enabled but not fail if it is
// It will check if the extension is installed
func EnableSystemExtension(cfg *config.Config, ext string, bootState string) error {
	// first check if the extension is installed
	extension, err := GetSystemExtension(cfg, ext, "")
	if err != nil {
		return err
	}

	var targetDir string
	switch bootState {
	case "active":
		targetDir = sysextDirActive
	case "passive":
		targetDir = sysextDirPassive
	default:
		return fmt.Errorf("boot state %s not supported", bootState)
	}

	// Check if the target dir exists and create it if it doesn't
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create target dir %s: %w", targetDir, err)
		}
	}

	// Check if the extension is already enabled
	enabled, err := os.Readlink(filepath.Join(targetDir, extension.Name))
	// This doesnt fail if we have it already enabled
	if err == nil {
		if enabled == extension.Location {
			cfg.Logger.Infof("System extension %s is already enabled in %s", extension.Name, bootState)
			return nil
		}
	}

	// Create a symlink to the extension in the target dir
	if err := os.Symlink(extension.Location, filepath.Join(targetDir, extension.Name)); err != nil {
		return fmt.Errorf("failed to create symlink for %s: %w", extension.Name, err)
	}
	cfg.Logger.Infof("System extension %s enabled in %s", extension.Name, bootState)
	return nil
}

// DisableSystemExtension disables a system extension that is already in the system for a given bootstate
// It removes the symlink from the target dir according to the bootstate given
func DisableSystemExtension(cfg *config.Config, ext string, bootState string) error {
	var targetDir string
	switch bootState {
	case "active":
		targetDir = sysextDirActive
	case "passive":
		targetDir = sysextDirPassive
	default:
		return fmt.Errorf("boot state %s not supported", bootState)
	}

	// Check if the target dir exists
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("target dir %s does not exist", targetDir)
	}

	// Check if the extension is enabled
	extension, err := GetSystemExtension(cfg, ext, bootState)
	if err != nil {
		return fmt.Errorf("system extension %s not enabled in %s: %w", ext, bootState, err)
	}

	// Remove the symlink
	if err := os.Remove(extension.Location); err != nil {
		return fmt.Errorf("failed to remove symlink for %s: %w", ext, err)
	}
	cfg.Logger.Infof("System extension %s disabled in %s", ext, bootState)
	return nil
}

// InstallSystemExtension installs a system extension from a given URI
// It will download the extension and extract it to the target dir
// It will check if the extension is already installed before doing anything
func InstallSystemExtension(cfg *config.Config, uri string) error {
	// Parse the URI
	download, err := parseURI(uri)
	if err != nil {
		return fmt.Errorf("failed to parse URI %s: %w", uri, err)
	}
	// Check if directory exists or create it
	if _, err := os.Stat(sysextDir); os.IsNotExist(err) {
		if err := os.MkdirAll(sysextDir, 0755); err != nil {
			return fmt.Errorf("failed to create target dir %s: %w", sysextDir, err)
		}
	}
	// Download the extension
	if err := download.Download(sysextDir); err != nil {
		return err
	}

	return nil
}

// RemoveSystemExtension removes a system extension from the system
// It will remove any symlinks to the extension
// Then it will remove the extension
// It will check if the extension is installed before doing anything
func RemoveSystemExtension(cfg *config.Config, extension string) error {
	// Check if the extension is installed
	installed, err := GetSystemExtension(cfg, extension, "")
	if err != nil {
		return err
	}
	// Check if the extension is enabled in active or passive
	enabledActive, err := GetSystemExtension(cfg, extension, "active")
	if err == nil {
		// Remove the symlink
		if err := os.Remove(enabledActive.Location); err != nil {
			return fmt.Errorf("failed to remove symlink for %s: %w", enabledActive.Name, err)
		}
		cfg.Logger.Infof("System extension %s disabled from active", enabledActive.Name)
	}
	enabledPassive, err := GetSystemExtension(cfg, extension, "passive")
	if err == nil {
		// Remove the symlink
		if err := os.Remove(enabledPassive.Location); err != nil {
			return fmt.Errorf("failed to remove symlink for %s: %w", enabledPassive.Name, err)
		}
		cfg.Logger.Infof("System extension %s disabled from passive", enabledPassive.Name)
	}
	// Remove the extension
	if err := os.RemoveAll(installed.Location); err != nil {
		return fmt.Errorf("failed to remove extension %s: %w", installed.Name, err)
	}
	cfg.Logger.Infof("System extension %s removed", installed.Name)
	return nil
}

// ParseURI parses a URI and returns a SourceDownload
// implementation based on the scheme of the URI
func parseURI(uri string) (SourceDownload, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	scheme := u.Scheme
	value := u.Opaque
	if value == "" {
		value = filepath.Join(u.Host, u.Path)
	}
	switch scheme {
	case "oci", "docker", "container":
		n, err := reference.ParseNormalizedNamed(value)
		if err != nil {
			return nil, fmt.Errorf("invalid image reference %s", value)
		} else if reference.IsNameOnly(n) {
			value += ":latest"
		}
		return &dockerSource{value}, nil
	case "file":
		return &fileSource{value}, nil
	case "http", "https":
		// Pass the full uri including the protocol
		return &httpSource{uri}, nil
	default:
		return nil, fmt.Errorf("invalid URI reference %s", uri)
	}
}

// SourceDownload is an interface for downloading system extensions
// from different sources. It allows for different implementations
// for different sources of system extensions, such as files, directories,
// or docker images. The interface defines a single method, Download,
// which takes a destination path as an argument and returns an error
type SourceDownload interface {
	Download(string) error
}

// fileSource is a struct that implements the SourceDownload interface
// for downloading system extensions from a file. It has a single field,
// uri, which is the URI of the file to be downloaded. The Download method
// takes a destination path as an argument and returns an error if the
// download fails.
type fileSource struct {
	uri string
}

// Download just copies the file to the destination
// As this is a file source, we just copy the file to the destination, not much to it
func (f *fileSource) Download(dst string) error {
	src, err := os.Open(f.uri)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", f.uri, err)
	}
	defer func(src *os.File) {
		_ = src.Close()
	}(src)

	dstFile, err := os.Create(filepath.Join(dst, filepath.Base(f.uri)))
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", dstFile.Name(), err)
	}
	defer func(dstFile *os.File) {
		_ = dstFile.Close()
	}(dstFile)

	if _, err := io.Copy(dstFile, src); err != nil {
		return fmt.Errorf("failed to copy file %s to %s: %w", f.uri, dstFile.Name(), err)
	}
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file %s: %w", dstFile.Name(), err)
	}

	return nil
}

type httpSource struct {
	uri string
}

func (h httpSource) Download(s string) error {
	// Download the file from the URI
	// and save it to the destination path
	client := http2.NewClient()
	return client.GetURL(types.NewNullLogger(), h.uri, filepath.Join(s, filepath.Base(h.uri)))
}

type dockerSource struct {
	uri string
}

func (d dockerSource) Download(s string) error {
	// Download the file from the URI
	// and save it to the destination path
	image, err := utils.GetImage(d.uri, "", nil, nil)
	if err != nil {
		return err
	}
	err = utils.ExtractOCIImage(image, s)
	if err != nil {
		return err
	}
	return nil
}
