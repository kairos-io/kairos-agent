/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	random "math/rand"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos-sdk/state"

	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"

	"github.com/distribution/distribution/reference"
	"github.com/joho/godotenv"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/twpayne/go-vfs"
)

func CommandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// GetDeviceByLabel will try to return the device that matches the given label.
// attempts value sets the number of attempts to find the device, it
// waits a second between attempts.
func GetDeviceByLabel(runner v1.Runner, label string, attempts int) (string, error) {
	part, err := GetFullDeviceByLabel(runner, label, attempts)
	if err != nil {
		return "", err
	}
	return part.Path, nil
}

// GetFullDeviceByLabel works like GetDeviceByLabel, but it will try to get as much info as possible from the existing
// partition and return a v1.Partition object
func GetFullDeviceByLabel(runner v1.Runner, label string, attempts int) (*v1.Partition, error) {
	for tries := 0; tries < attempts; tries++ {
		_, _ = runner.Run("udevadm", "settle")
		parts, err := partitions.GetAllPartitions()
		if err != nil {
			return nil, err
		}
		part := parts.GetByLabel(label)
		if part != nil {
			return part, nil
		}
		time.Sleep(1 * time.Second)
	}
	return nil, errors.New("no device found")
}

// CopyFile Copies source file to target file using Fs interface. If target
// is  directory source is copied into that directory using source name file.
func CopyFile(fs v1.FS, source string, target string) (err error) {
	return ConcatFiles(fs, []string{source}, target)
}

// ConcatFiles Copies source files to target file using Fs interface.
// Source files are concatenated into target file in the given order.
// If target is a directory source is copied into that directory using
// 1st source name file.
func ConcatFiles(fs v1.FS, sources []string, target string) (err error) {
	if len(sources) == 0 {
		return fmt.Errorf("Empty sources list")
	}
	if dir, _ := fsutils.IsDir(fs, target); dir {
		target = filepath.Join(target, filepath.Base(sources[0]))
	}

	targetFile, err := fs.Create(target)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = targetFile.Close()
		} else {
			_ = fs.Remove(target)
		}
	}()

	var sourceFile *os.File
	for _, source := range sources {
		sourceFile, err = fs.Open(source)
		if err != nil {
			break
		}
		_, err = io.Copy(targetFile, sourceFile)
		if err != nil {
			break
		}
		err = sourceFile.Close()
		if err != nil {
			break
		}
	}

	return err
}

// Copies source file to target file using Fs interface
func CreateDirStructure(fs v1.FS, target string) error {
	for _, dir := range []string{"/run", "/dev", "/boot", "/usr/local", "/oem"} {
		err := fsutils.MkdirAll(fs, filepath.Join(target, dir), cnst.DirPerm)
		if err != nil {
			return err
		}
	}
	for _, dir := range []string{"/proc", "/sys"} {
		err := fsutils.MkdirAll(fs, filepath.Join(target, dir), cnst.NoWriteDirPerm)
		if err != nil {
			return err
		}
	}
	err := fsutils.MkdirAll(fs, filepath.Join(target, "/tmp"), cnst.DirPerm)
	if err != nil {
		return err
	}
	// Set /tmp permissions regardless the umask setup
	err = fs.Chmod(filepath.Join(target, "/tmp"), cnst.TempDirPerm)
	if err != nil {
		return err
	}
	return nil
}

// SyncData rsync's source folder contents to a target folder content,
// both are expected to exist beforehand.
func SyncData(log v1.Logger, runner v1.Runner, fs v1.FS, source string, target string, excludes ...string) error {
	if fs != nil {
		if s, err := fs.RawPath(source); err == nil {
			source = s
		}
		if t, err := fs.RawPath(target); err == nil {
			target = t
		}
	}

	if !strings.HasSuffix(source, "/") {
		source = fmt.Sprintf("%s/", source)
	}

	if !strings.HasSuffix(target, "/") {
		target = fmt.Sprintf("%s/", target)
	}

	log.Infof("Starting rsync...")
	args := []string{"--progress", "--partial", "--human-readable", "--archive", "--xattrs", "--acls"}

	for _, e := range excludes {
		args = append(args, fmt.Sprintf("--exclude=%s", e))
	}

	args = append(args, source, target)

	done := displayProgress(log, 5*time.Second, "Syncing data...")

	out, err := runner.Run(cnst.Rsync, args...)

	close(done)
	if err != nil {
		log.Errorf("rsync finished with errors: %s, %s", err.Error(), string(out))
		return err
	}
	log.Info("Finished syncing")

	return nil
}

func displayProgress(log v1.Logger, tick time.Duration, message string) chan bool {
	ticker := time.NewTicker(tick)
	done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				ticker.Stop()
				return
			case <-ticker.C:
				log.Debug(message)
			}
		}
	}()

	return done
}

// Reboot reboots the system afater the given delay (in seconds) time passed.
func Reboot(runner v1.Runner, delay time.Duration) error {
	time.Sleep(delay * time.Second)
	_, err := runner.Run("reboot", "-f")
	return err
}

// Shutdown halts the system afater the given delay (in seconds) time passed.
func Shutdown(runner v1.Runner, delay time.Duration) error {
	time.Sleep(delay * time.Second)
	_, err := runner.Run("poweroff", "-f")
	return err
}

// CosignVerify runs a cosign validation for the give image and given public key. If no
// key is provided then it attempts a keyless validation (experimental feature).
func CosignVerify(fs v1.FS, runner v1.Runner, image string, publicKey string, debug bool) (string, error) {
	args := []string{}

	if debug {
		args = append(args, "-d=true")
	}
	if publicKey != "" {
		args = append(args, "-key", publicKey)
	} else {
		os.Setenv("COSIGN_EXPERIMENTAL", "1")
		defer os.Unsetenv("COSIGN_EXPERIMENTAL")
	}
	args = append(args, image)

	// Give each cosign its own tuf dir so it doesnt collide with others accessing the same files at the same time
	tmpDir, err := fsutils.TempDir(fs, "", "cosign-tuf-")
	if err != nil {
		return "", err
	}
	_ = os.Setenv("TUF_ROOT", tmpDir)
	defer func(fs v1.FS, path string) {
		_ = fs.RemoveAll(path)
	}(fs, tmpDir)
	defer func() {
		_ = os.Unsetenv("TUF_ROOT")
	}()

	out, err := runner.Run("cosign", args...)
	return string(out), err
}

// CreateSquashFS creates a squash file at destination from a source, with options
// TODO: Check validity of source maybe?
func CreateSquashFS(runner v1.Runner, logger v1.Logger, source string, destination string, options []string) error {
	// create args
	args := []string{source, destination}
	// append options passed to args in order to have the correct order
	// protect against options passed together in the same string , i.e. "-x add" instead of "-x", "add"
	var optionsExpanded []string
	for _, op := range options {
		optionsExpanded = append(optionsExpanded, strings.Split(op, " ")...)
	}
	args = append(args, optionsExpanded...)
	out, err := runner.Run("mksquashfs", args...)
	if err != nil {
		logger.Debugf("Error running squashfs creation, stdout: %s", out)
		logger.Errorf("Error while creating squashfs from %s to %s: %s", source, destination, err)
		return err
	}
	return nil
}

// LoadEnvFile will try to parse the file given and return a map with the kye/values
func LoadEnvFile(fs v1.FS, file string) (map[string]string, error) {
	var envMap map[string]string
	var err error

	f, err := fs.Open(file)
	if err != nil {
		return envMap, err
	}
	defer f.Close()

	envMap, err = godotenv.Parse(f)
	if err != nil {
		return envMap, err
	}

	return envMap, err
}

func IsMounted(config *agentConfig.Config, part *v1.Partition) (bool, error) {
	if part == nil {
		return false, fmt.Errorf("nil partition")
	}

	if part.MountPoint == "" {
		return false, nil
	}
	// Using IsLikelyNotMountPoint seams to be safe as we are not checking
	// for bind mounts here
	notMnt, err := config.Mounter.IsLikelyNotMountPoint(part.MountPoint)
	if err != nil {
		return false, err
	}
	return !notMnt, nil
}

// GetTempDir returns the dir for storing related temporal files
// It will respect TMPDIR and use that if exists, fallback to try the persistent partition if its mounted
// and finally the default /tmp/ dir
// suffix is what is appended to the dir name elemental-suffix. If empty it will randomly generate a number
func GetTempDir(config *agentConfig.Config, suffix string) string {
	// if we got a TMPDIR var, respect and use that
	if suffix == "" {
		random.Seed(time.Now().UnixNano())
		suffix = strconv.Itoa(int(random.Uint32()))
	}
	elementalTmpDir := fmt.Sprintf("elemental-%s", suffix)
	dir := os.Getenv("TMPDIR")
	if dir != "" {
		config.Logger.Debugf("Got tmpdir from TMPDIR var: %s", dir)
		return filepath.Join(dir, elementalTmpDir)
	}
	parts, err := partitions.GetAllPartitions()
	if err != nil {
		config.Logger.Debug("Could not get partitions, defaulting to /tmp")
		return filepath.Join("/", "tmp", elementalTmpDir)
	}
	// Check persistent and if its mounted
	ep := v1.NewElementalPartitionsFromList(parts)
	persistent := ep.Persistent
	if persistent != nil {
		if mnt, _ := IsMounted(config, persistent); mnt {
			config.Logger.Debugf("Using tmpdir on persistent volume: %s", persistent.MountPoint)
			return filepath.Join(persistent.MountPoint, "tmp", elementalTmpDir)
		}
	}
	config.Logger.Debug("Could not get any valid tmpdir, defaulting to /tmp")
	return filepath.Join("/", "tmp", elementalTmpDir)
}

// IsLocalURI returns true if the uri has "file" scheme or no scheme and URI is
// not prefixed with a domain (container registry style). Returns false otherwise.
// Error is not nil only if the url can't be parsed.
func IsLocalURI(uri string) (bool, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return false, err
	}
	if u.Scheme == "file" {
		return true, nil
	}
	if u.Scheme == "" {
		// Check first part of the path is not a domain (e.g. registry.suse.com/elemental)
		// reference.ParsedNamed expects a <domain>[:<port>]/<path>[:<tag>] form.
		if _, err = reference.ParseNamed(uri); err != nil {
			return true, nil
		}
	}
	return false, nil
}

// IsHTTPURI returns true if the uri has "http" or "https" scheme, returns false otherwise.
// Error is not nil only if the url can't be parsed.
func IsHTTPURI(uri string) (bool, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return false, err
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return true, nil
	}
	return false, nil
}

// GetSource copies given source to destination, if source is a local path it simply
// copies files, if source is a remote URL it tries to download URL to destination.
func GetSource(config *agentConfig.Config, source string, destination string) error {
	local, err := IsLocalURI(source)
	if err != nil {
		config.Logger.Errorf("Not a valid url: %s", source)
		return err
	}

	err = vfs.MkdirAll(config.Fs, filepath.Dir(destination), cnst.DirPerm)
	if err != nil {
		config.Logger.Debugf("Failed creating dir %s: %s\n", filepath.Dir(destination), err.Error())
		return err
	}
	if local {
		u, _ := url.Parse(source)
		err = CopyFile(config.Fs, u.Path, destination)
		if err != nil {
			config.Logger.Debugf("error copying source from %s to %s: %s\n", source, destination, err.Error())
			return err
		}
	} else {
		err = config.Client.GetURL(config.Logger, source, destination)
		if err != nil {
			return err
		}
	}
	return nil
}

// ValidContainerReferece returns true if the given string matches
// a container registry reference, false otherwise
func ValidContainerReference(ref string) bool {
	if _, err := reference.ParseNormalizedNamed(ref); err != nil {
		return false
	}
	return true
}

// ValidTaggedContainerReferece returns true if the given string matches
// a container registry reference including a tag, false otherwise.
func ValidTaggedContainerReference(ref string) bool {
	n, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return false
	}
	if reference.IsNameOnly(n) {
		return false
	}
	return true
}

// FindFileWithPrefix looks for a file in the given path matching one of the given
// prefixes. Returns the found file path including the given path. It does not
// check subfolders recusively
func FindFileWithPrefix(fs v1.FS, path string, prefixes ...string) (string, error) {
	files, err := fs.ReadDir(path)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		for _, p := range prefixes {
			if strings.HasPrefix(f.Name(), p) {
				if f.Mode()&os.ModeSymlink == os.ModeSymlink {
					found, err := fs.Readlink(filepath.Join(path, f.Name()))
					if err == nil {
						if !filepath.IsAbs(found) {
							found = filepath.Join(path, found)
						}
						if exists, _ := fsutils.Exists(fs, found); exists {
							return found, nil
						}
					}
				} else {
					return filepath.Join(path, f.Name()), nil
				}
			}
		}
	}
	return "", fmt.Errorf("No file found with prefixes: %v", prefixes)
}

// CalcFileChecksum opens the given file and returns the sha256 checksum of it.
func CalcFileChecksum(fs v1.FS, fileName string) (string, error) {
	f, err := fs.Open(fileName)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// FindCommand will search for the command(s) in the options given to find the current command
// If it cant find it returns the default value give. Useful for the same binaries with different names across OS
func FindCommand(defaultPath string, options []string) string {
	for _, p := range options {
		path, err := exec.LookPath(p)
		if err == nil {
			return path
		}
	}

	// Otherwise return default
	return defaultPath
}

// IsUki returns true if the system is running in UKI mode. Checks the cmdline as UKI artifacts have the rd.immucore.uki flag
func IsUki() bool {
	cmdline, _ := os.ReadFile("/proc/cmdline")
	if strings.Contains(string(cmdline), "rd.immucore.uki") {
		return true
	}
	return false
}

// IsUkiWithFs checks if the system is running in UKI mode
// by checking the kernel command line for the rd.immucore.uki flag
// Uses a v1.Fs interface to allow for testing
func IsUkiWithFs(fs v1.FS) bool {
	cmdline, _ := fs.ReadFile("/proc/cmdline")
	if strings.Contains(string(cmdline), "rd.immucore.uki") {
		return true
	}
	return false
}

const (
	UkiHDD            state.Boot = "uki_boot_mode"
	UkiRemovableMedia state.Boot = "uki_install_mode"
)

// UkiBootMode will return where the system is running from, either HDD or RemovableMedia
// HDD means we are booting from an already installed system
// RemovableMedia means we are booting from a live media like a CD or USB
func UkiBootMode() state.Boot {
	if IsUki() {
		_, err := os.Stat("/run/cos/uki_boot_mode")
		if err == nil {
			return UkiHDD
		}
		return UkiRemovableMedia
	}
	return state.Unknown
}

// SystemdBootConfReader reads a systemd-boot conf file and returns a map with the key/value pairs
func SystemdBootConfReader(fs v1.FS, filePath string) (map[string]string, error) {
	file, err := fs.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
		if len(parts) == 1 {
			result[parts[0]] = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// SystemdBootConfWriter writes a map to a systemd-boot conf file
func SystemdBootConfWriter(fs v1.FS, filePath string, conf map[string]string) error {
	file, err := fs.Create(filePath)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	writer := bufio.NewWriter(file)
	for k, v := range conf {
		if v == "" {
			_, err = writer.WriteString(fmt.Sprintf("%s \n", k))
		} else {
			_, err = writer.WriteString(fmt.Sprintf("%s %s\n", k, v))
		}
		if err != nil {
			return err
		}
	}

	return writer.Flush()
}
