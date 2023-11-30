package hook

import (
	"fmt"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/utils"
	kcrypt "github.com/kairos-io/kcrypt/pkg/lib"
)

type Kcrypt struct{}

func (k Kcrypt) Run(c config.Config, _ v1.Spec) error {

	if len(c.Install.Encrypt) == 0 {
		return nil
	}

	// Config passed during install ends up here, so we need to read it
	_ = machine.Mount("COS_OEM", "/oem")
	defer func() {
		_ = machine.Umount("/oem") //nolint:errcheck
	}()

	for _, p := range c.Install.Encrypt {
		out, err := utils.SH(fmt.Sprintf("kcrypt encrypt %s", p))
		if err != nil {
			fmt.Printf("could not encrypt partition: %s\n", out+err.Error())
			if c.FailOnBundleErrors {
				return err
			}
			// Give time to show the error
			time.Sleep(10 * time.Second)
			return nil // do not error out
		}
	}

	return nil
}

type KcryptUKI struct{}

func (k KcryptUKI) Run(c config.Config, _ v1.Spec) error {

	// We always encrypt OEM and PERSISTENT under UKI

	// Backup oem as we already copied files on there and on luksify it will be wiped
	err := machine.Mount("COS_OEM", "/oem")
	if err != nil {
		return err
	}
	tmpDir, err := fsutils.TempDir(c.Fs, "", "oem-backup-xxxx")
	if err != nil {
		return err
	}

	// Remove everything when we finish
	defer c.Fs.RemoveAll(tmpDir) //nolint:errcheck

	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, "/oem", tmpDir, []string{}...)
	if err != nil {
		return err
	}
	err = machine.Umount("/oem") //nolint:errcheck
	if err != nil {
		return err
	}

	for _, p := range []string{"COS_OEM", "COS_PERSISTENT"} {
		// USE kcrypt lib directly???
		out, err := utils.SH(fmt.Sprintf("kcrypt encrypt --version luks2 --tpm %s ", p))
		if err != nil {
			fmt.Printf("could not encrypt partition: %s\n", out+err.Error())
			if c.FailOnBundleErrors {
				return err
			}
			// Give time to show the error
			time.Sleep(10 * time.Second)
			return nil // do not error out
		}
	}

	// Restore OEM
	err = kcrypt.UnlockAll(true)
	if err != nil {
		return err
	}
	err = machine.Mount("COS_OEM", "/oem")
	if err != nil {
		return err
	}
	err = internalutils.SyncData(c.Logger, c.Runner, c.Fs, tmpDir, "/oem", []string{}...)
	if err != nil {
		return err
	}
	err = machine.Umount("/oem") //nolint:errcheck
	if err != nil {
		return err
	}
	return nil
}
