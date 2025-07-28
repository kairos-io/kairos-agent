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

package action

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"path/filepath"
)

// Hook is RunStage wrapper that only adds logic to ignore errors
// in case v1.Config.Strict is set to false
func Hook(config *config.Config, hook string) error {
	config.Logger.Infof("Running %s hook", hook)
	err := utils.RunStage(config, hook)
	if !config.Strict {
		err = nil
	}
	return err
}

// ChrootHook executes Hook inside a chroot environment
func ChrootHook(config *config.Config, hook string, chrootDir string, bindMounts map[string]string) (err error) {
	callback := func() error {
		return Hook(config, hook)
	}
	return utils.ChrootedCallback(config, chrootDir, bindMounts, callback)
}

func createExtraDirsInRootfs(cfg *config.Config, extradirs []string, target string) {
	if target == "" {
		cfg.Logger.Warn("Empty target for extra rootfs dirs, not doing anything")
		return
	}

	for _, d := range extradirs {
		if exists, _ := fsutils.Exists(cfg.Fs, filepath.Join(target, d)); !exists {
			cfg.Logger.Debugf("Creating extra dir %s under %s", d, target)
			err := fsutils.MkdirAll(cfg.Fs, filepath.Join(target, d), cnst.DirPerm)
			if err != nil {
				cfg.Logger.Warnf("Failure creating extra dir %s under %s", d, target)
			}
		}
	}
}

// SetDefaultGrubEntry Sets the default_menu_entry value in Config.GrubOEMEnv file at in
// State partition mountpoint. If there is not a custom value in the kairos-release file, we do nothing
// As the grub config already has a sane default
func SetDefaultGrubEntry(config *config.Config, partMountPoint string, imgMountPoint string, defaultEntry string) error {
	if defaultEntry == "" {
		var osRelease map[string]string
		osRelease, err := utils.LoadEnvFile(config.Fs, filepath.Join(imgMountPoint, "etc", "kairos-release"))
		if err != nil {
			// Fallback to os-release
			osRelease, err = utils.LoadEnvFile(config.Fs, filepath.Join(imgMountPoint, "etc", "os-release"))
			config.Logger.Warnf("Could not load os-release file: %v", err)
			return nil
		}
		defaultEntry = osRelease["GRUB_ENTRY_NAME"]
		// If its still empty then do nothing
		if defaultEntry == "" {
			return nil
		}
	}
	config.Logger.Infof("Setting default grub entry to %s", defaultEntry)
	return utils.SetPersistentVariables(
		filepath.Join(partMountPoint, cnst.GrubOEMEnv),
		map[string]string{"default_menu_entry": defaultEntry},
		config,
	)
}
