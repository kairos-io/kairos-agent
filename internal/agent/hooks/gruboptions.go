package hook

import (
	"fmt"
	"strings"

	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
)

// GrubPostInstallOptions is a hook that runs after the install process to add grub options.
type GrubPostInstallOptions struct{}

func (b GrubPostInstallOptions) Run(c config.Config, _ v1.Spec) error {
	// Combine regular grub options with extracted kcrypt options
	grubOpts := make(map[string]string)

	// Copy existing grub options
	for k, v := range c.Install.GrubOptions {
		grubOpts[k] = v
	}

	// Check if COS_OEM is in the list of encrypted partitions
	oemEncrypted := false
	if len(c.Install.Encrypt) > 0 {
		for _, part := range c.Install.Encrypt {
			if part == cnst.OEMLabel {
				oemEncrypted = true
				break
			}
		}
	}

	// Extract and add kcrypt.challenger settings to cmdline if COS_OEM is encrypted
	// This solves the chicken-egg problem where kcrypt config is on the encrypted OEM partition
	if oemEncrypted {
		c.Logger.Logger.Info().Msg("COS_OEM is encrypted, extracting kcrypt.challenger config to cmdline")
		kcryptCmdline := extractKcryptCmdline(&c)
		if kcryptCmdline != "" {
			// Append to extra_cmdline
			if existing, ok := grubOpts["extra_cmdline"]; ok {
				grubOpts["extra_cmdline"] = existing + " " + kcryptCmdline
			} else {
				grubOpts["extra_cmdline"] = kcryptCmdline
			}
			c.Logger.Logger.Info().Str("kcrypt_cmdline", kcryptCmdline).Msg("Added kcrypt config to cmdline")
		}
	}

	if len(grubOpts) == 0 {
		return nil
	}

	c.Logger.Logger.Info().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", grubOpts)
	err := grubOptions(c, grubOpts, oemEncrypted)
	if err != nil {
		return err
	}
	c.Logger.Logger.Info().Msg("Finish GrubOptions hook")
	return nil
}

// GrubFirstBootOptions is a hook that runs on the first boot to add grub options.
type GrubFirstBootOptions struct{}

func (b GrubFirstBootOptions) Run(c config.Config, _ v1.Spec) error {
	if len(c.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Logger.Info().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", c.GrubOptions)
	// At first boot, we don't know if OEM is encrypted, so write to both as a fallback
	err := grubOptions(c, c.GrubOptions, false)
	if err != nil {
		return err
	}
	c.Logger.Logger.Info().Msg("Finish GrubOptions hook")
	return nil
}

// grubOptions sets the grub options in the grubenv file
// It ALWAYS writes to STATE partition (grubenv) as that's what GRUB reads
// It optionally writes to OEM partition (grub_oem_env) only if OEM is not encrypted
func grubOptions(c config.Config, opts map[string]string, oemEncrypted bool) error {
	var firstErr error

	// Always write to STATE partition (grubenv) - this is what GRUB reads during boot
	_ = machine.Umount(cnst.StateDir)
	c.Logger.Logger.Debug().Msg("Mounting STATE partition")
	_ = machine.Mount(cnst.StateLabel, cnst.StateDir)
	defer func() {
		c.Logger.Logger.Debug().Msg("Unmounting STATE partition")
		_ = machine.Umount(cnst.StateDir)
	}()

	err := utils.SetPersistentVariables(filepath.Join(cnst.StateDir, cnst.GrubEnv), opts, &c)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Str("grubfile", filepath.Join(cnst.StateDir, cnst.GrubEnv)).Msg("Failed to set grub options in STATE")
		firstErr = err
	} else {
		c.Logger.Logger.Info().Str("grubfile", filepath.Join(cnst.StateDir, cnst.GrubEnv)).Msg("Successfully set grub options in STATE")
	}

	// Only write to OEM if it's not encrypted (GRUB can't read it if encrypted anyway)
	if !oemEncrypted {
		_ = machine.Umount(cnst.OEMDir)
		_ = machine.Umount(cnst.OEMPath)

		c.Logger.Logger.Debug().Msg("Mounting OEM partition")
		_ = machine.Mount(cnst.OEMLabel, cnst.OEMPath)

		err = utils.SetPersistentVariables(filepath.Join(cnst.OEMPath, cnst.GrubOEMEnv), opts, &c)
		if err != nil {
			c.Logger.Logger.Warn().Err(err).Str("grubfile", filepath.Join(cnst.OEMPath, cnst.GrubOEMEnv)).Msg("Failed to set grub options in OEM (non-critical)")
		} else {
			c.Logger.Logger.Info().Str("grubfile", filepath.Join(cnst.OEMPath, cnst.GrubOEMEnv)).Msg("Successfully set grub options in OEM")
		}

		_ = machine.Umount(cnst.OEMPath)
	} else {
		c.Logger.Logger.Info().Msg("Skipping OEM grubenv write as OEM partition is encrypted")
	}

	return firstErr
}

// extractKcryptCmdline extracts kcrypt.challenger config from the Kairos config and
// formats it as kernel command line arguments for use in grub.
// This allows kcrypt-challenger to access KMS settings even when COS_OEM is encrypted.
func extractKcryptCmdline(c *config.Config) string {
	var cmdlineArgs []string

	// Access the generic config values map to get kcrypt settings
	if c.Config.Values == nil {
		return ""
	}

	kcryptVal, hasKcrypt := c.Config.Values["kcrypt"]
	if !hasKcrypt {
		return ""
	}

	// Type assert to access nested structure
	kcryptMap, ok := kcryptVal.(collector.ConfigValues)
	if !ok {
		c.Logger.Logger.Debug().Msg("kcrypt config is not in expected format")
		return ""
	}

	challengerVal, hasChallengerKey := kcryptMap["challenger"]
	if !hasChallengerKey {
		return ""
	}

	challengerMap, ok := challengerVal.(collector.ConfigValues)
	if !ok {
		c.Logger.Logger.Debug().Msg("kcrypt.challenger config is not in expected format")
		return ""
	}

	// Extract individual settings and add as cmdline parameters
	// Using kairos.kcrypt.challenger.* prefix to match the expected config structure

	if server, ok := challengerMap["challenger_server"].(string); ok && server != "" {
		// URL encode any special characters in the server URL
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kairos.kcrypt.challenger.challenger_server=%s", server))
	}

	if mdns, ok := challengerMap["mdns"].(bool); ok && mdns {
		cmdlineArgs = append(cmdlineArgs, "kairos.kcrypt.challenger.mdns=true")
	}

	if cert, ok := challengerMap["certificate"].(string); ok && cert != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kairos.kcrypt.challenger.certificate=%s", cert))
	}

	if nvIndex, ok := challengerMap["nv_index"].(string); ok && nvIndex != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kairos.kcrypt.challenger.nv_index=%s", nvIndex))
	}

	if cIndex, ok := challengerMap["c_index"].(string); ok && cIndex != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kairos.kcrypt.challenger.c_index=%s", cIndex))
	}

	if tpmDevice, ok := challengerMap["tpm_device"].(string); ok && tpmDevice != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kairos.kcrypt.challenger.tpm_device=%s", tpmDevice))
	}

	return strings.Join(cmdlineArgs, " ")
}
