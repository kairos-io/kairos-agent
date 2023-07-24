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
	"fmt"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	fs2 "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
)

// Grub is the struct that will allow us to install grub to the target device
type Grub struct {
	config *agentConfig.Config
}

func NewGrub(config *agentConfig.Config) *Grub {
	g := &Grub{
		config: config,
	}

	return g
}

// Install installs grub into the device, copy the config file and add any extra TTY to grub
func (g Grub) Install(target, rootDir, bootDir, grubConf, tty string, efi bool, stateLabel string) (err error) { // nolint:gocyclo
	var grubargs []string
	var grubdir, finalContent string

	// At this point the active mountpoint has all the data from the installation source, so we should be able to use
	// the grub.cfg bundled in there
	systemgrub := "grub2"

	// only install grub on non-efi systems
	if !efi {
		g.config.Logger.Info("Installing GRUB..")

		grubargs = append(
			grubargs,
			fmt.Sprintf("--root-directory=%s", rootDir),
			fmt.Sprintf("--boot-directory=%s", bootDir),
			"--target=i386-pc",
			target,
		)

		g.config.Logger.Debugf("Running grub with the following args: %s", grubargs)
		out, err := g.config.Runner.Run(FindCommand("grub2-install", []string{"grub2-install", "grub-install"}), grubargs...)
		if err != nil {
			g.config.Logger.Errorf(string(out))
			return err
		}
		g.config.Logger.Infof("Grub install to device %s complete", target)

		// Select the proper dir for grub - this assumes that we previously run a grub install command, which is not the case in EFI
		// In the EFI case we default to grub2
		if ok, _ := fs2.IsDir(g.config.Fs, filepath.Join(bootDir, "grub")); ok {
			systemgrub = "grub"
		}
	}

	grubdir = filepath.Join(rootDir, grubConf)
	g.config.Logger.Infof("Using grub config dir %s", grubdir)

	grubCfg, err := g.config.Fs.ReadFile(grubdir)
	if err != nil {
		g.config.Logger.Errorf("Failed reading grub config file: %s", filepath.Join(rootDir, grubConf))
		return err
	}

	// Create Needed dir under state partition to store the grub.cfg and any needed modules
	err = fs2.MkdirAll(g.config.Fs, filepath.Join(bootDir, fmt.Sprintf("%s/%s-efi", systemgrub, g.config.Arch)), cnst.DirPerm)
	if err != nil {
		return fmt.Errorf("error creating grub dir: %s", err)
	}

	grubConfTarget, err := g.config.Fs.Create(filepath.Join(bootDir, fmt.Sprintf("%s/grub.cfg", systemgrub)))
	if err != nil {
		return err
	}

	defer grubConfTarget.Close()

	if tty == "" {
		// Get current tty and remove /dev/ from its name
		out, err := g.config.Runner.Run("tty")
		tty = strings.TrimPrefix(strings.TrimSpace(string(out)), "/dev/")
		if err != nil {
			g.config.Logger.Warnf("failed to find current tty, leaving it unset")
			tty = ""
		}
	}

	ttyExists, _ := fs2.Exists(g.config.Fs, fmt.Sprintf("/dev/%s", tty))

	if ttyExists && tty != "" && tty != "console" && tty != constants.DefaultTty {
		// We need to add a tty to the grub file
		g.config.Logger.Infof("Adding extra tty (%s) to grub.cfg", tty)
		defConsole := fmt.Sprintf("console=%s", constants.DefaultTty)
		finalContent = strings.Replace(string(grubCfg), defConsole, fmt.Sprintf("%s console=%s", defConsole, tty), -1)
	} else {
		// We don't add anything, just read the file
		finalContent = string(grubCfg)
	}

	g.config.Logger.Infof("Copying grub contents from %s to %s", grubdir, filepath.Join(bootDir, fmt.Sprintf("%s/grub.cfg", systemgrub)))
	_, err = grubConfTarget.WriteString(finalContent)
	if err != nil {
		return err
	}

	if efi {
		// Copy required extra modules to boot dir under the state partition
		// otherwise if we insmod it will fail to find them
		// We no longer call grub-install here so the modules are not setup automatically in the state partition
		// as they were before. We now use the bundled grub.efi provided by the shim package
		g.config.Logger.Infof("Generating grub files for efi on %s", target)
		var foundModules bool
		for _, m := range []string{"loopback.mod", "squash4.mod", "xzio.mod", "gzio.mod"} {
			err = fs2.WalkDirFs(g.config.Fs, rootDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.Name() == m && strings.Contains(path, g.config.Arch) {
					fileWriteName := filepath.Join(bootDir, fmt.Sprintf("%s/%s-efi/%s", systemgrub, g.config.Arch, m))
					g.config.Logger.Debugf("Copying %s to %s", path, fileWriteName)
					fileContent, err := g.config.Fs.ReadFile(path)
					if err != nil {
						return fmt.Errorf("error reading %s: %s", path, err)
					}
					err = g.config.Fs.WriteFile(fileWriteName, fileContent, cnst.FilePerm)
					if err != nil {
						return fmt.Errorf("error writing %s: %s", fileWriteName, err)
					}
					foundModules = true
					return nil
				}
				return err
			})
			if !foundModules {
				return fmt.Errorf("did not find grub modules under %s", rootDir)
			}
		}

		err = fs2.MkdirAll(g.config.Fs, filepath.Join(cnst.EfiDir, "EFI/boot/"), cnst.DirPerm)
		if err != nil {
			g.config.Logger.Errorf("Error creating dirs: %s", err)
			return err
		}

		// Copy needed files for efi boot
		shimFiles := cnst.GetGrubFilePaths(g.config.Arch)

		for _, f := range shimFiles {
			_, name := filepath.Split(f)
			fileWriteName := filepath.Join(cnst.EfiDir, fmt.Sprintf("EFI/boot/%s", name))
			g.config.Logger.Debugf("Copying %s to %s", f, fileWriteName)

			// They are bundled in the rootfs so pick them from there
			fileContent, err := g.config.Fs.ReadFile(filepath.Join(cnst.ActiveDir, f))

			if err != nil {
				return fmt.Errorf("error reading %s: %s", filepath.Join(cnst.ActiveDir, f), err)
			}
			err = g.config.Fs.WriteFile(fileWriteName, fileContent, cnst.FilePerm)
			if err != nil {
				return fmt.Errorf("error writing %s: %s", fileWriteName, err)
			}
		}

		// Rename the shimName to the fallback name so the system boots from fallback. This means that we do not create
		// any bootloader entries, so our recent installation has the lower priority if something else is on the bootloader
		writeShim := cnst.GetFallBackEfi(g.config.Arch)

		readShim, err := g.config.Fs.ReadFile(filepath.Join(cnst.EfiDir, "EFI/boot/", cnst.SignedShim))
		if err != nil {
			return fmt.Errorf("could not read shim file %s at dir %s", cnst.SignedShim, cnst.EfiDir)
		}

		err = g.config.Fs.WriteFile(filepath.Join(cnst.EfiDir, "EFI/boot/", writeShim), readShim, cnst.FilePerm)
		if err != nil {
			return fmt.Errorf("could not write shim file %s at dir %s", writeShim, cnst.EfiDir)
		}

		// Add grub.cfg in EFI that chainloads the grub.cfg in recovery
		// Notice that we set the config to /grub2/grub.cfg which means the above we need to copy the file from
		// the installation source into that dir
		grubCfgContent := []byte(fmt.Sprintf("search --no-floppy --label --set=root %s\nset prefix=($root)/%s\nconfigfile ($root)/%s/grub.cfg", stateLabel, systemgrub, systemgrub))
		err = g.config.Fs.WriteFile(filepath.Join(cnst.EfiDir, "EFI/boot/grub.cfg"), grubCfgContent, cnst.FilePerm)
		if err != nil {
			return fmt.Errorf("error writing %s: %s", filepath.Join(cnst.EfiDir, "EFI/boot/grub.cfg"), err)
		}
	}

	return nil
}

// Sets the given key value pairs into as grub variables into the given file
func (g Grub) SetPersistentVariables(grubEnvFile string, vars map[string]string) error {
	for key, value := range vars {
		g.config.Logger.Debugf("Running grub2-editenv with params: %s set %s=%s", grubEnvFile, key, value)
		out, err := g.config.Runner.Run(FindCommand("grub2-editenv", []string{"grub2-editenv", "grub-editenv"}), grubEnvFile, "set", fmt.Sprintf("%s=%s", key, value))
		if err != nil {
			g.config.Logger.Errorf(fmt.Sprintf("Failed setting grub variables: %s", out))
			return err
		}
	}
	return nil
}
