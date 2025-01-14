package hook

import (
	"fmt"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/mudler/yip/pkg/schema"
	yip "github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v3"
)

type CustomMounts struct{}

func saveCloudConfig(name config.Stage, yc yip.YipConfig) error {
	yipYAML, err := yaml.Marshal(yc)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join("/oem", fmt.Sprintf("10_%s.yaml", name)), yipYAML, 0400)
}

// Run Reads the keys sections ephemeral_mounts and bind mounts from install key in the cloud config.
// If not empty write an environment file to /run/cos/custom-layout.env.
// That env file is in turn read by /overlay/files/system/oem/11_persistency.yaml in fs.after stage.
func (cm CustomMounts) Run(c config.Config, _ v1.Spec) error {

	//fmt.Println("Custom mounts hook")
	//fmt.Println(strings.Join(c.Install.BindMounts, " "))
	//fmt.Println(strings.Join(c.Install.EphemeralMounts, " "))

	if len(c.Install.BindMounts) == 0 && len(c.Install.EphemeralMounts) == 0 {
		return nil
	}
	c.Logger.Logger.Debug().Msg("Running CustomMounts hook")

	machine.Mount("COS_OEM", "/oem") //nolint:errcheck
	defer func() {
		machine.Umount("/oem") //nolint:errcheck
	}()

	var mountsList = map[string]string{}

	mountsList["CUSTOM_BIND_MOUNTS"] = strings.Join(c.Install.BindMounts, " ")
	mountsList["CUSTOM_EPHEMERAL_MOUNTS"] = strings.Join(c.Install.EphemeralMounts, " ")

	cfg := yip.YipConfig{Stages: map[string][]schema.Stage{
		"rootfs": {
			{
				Name:            "user_custom_mounts",
				EnvironmentFile: "/run/cos/custom-layout.env",
				Environment:     mountsList,
			},
		},
	}}

	err := saveCloudConfig("user_custom_mounts", cfg)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Msg("Failed to save cloud config")
		return err
	}
	c.Logger.Logger.Debug().Msg("Finish CustomMounts hook")
	return nil
}
