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
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
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
