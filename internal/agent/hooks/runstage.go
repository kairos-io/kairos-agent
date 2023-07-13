package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/elementalConfig"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"

	events "github.com/kairos-io/kairos-sdk/bus"
)

type RunStage struct{}

func (r RunStage) Run(c config.Config) error {
	cfg, err := elementalConfig.ReadConfigRunFromAgentConfig(&c)
	if err != nil {
		cfg.Logger.Errorf("Error reading config: %s\n", err)
	}
	_ = utils.RunStage(&cfg.Config, "kairos-install.after", cfg.Strict, cfg.CloudInitPaths...)
	events.RunHookScript("/usr/bin/kairos-agent.install.after.hook") //nolint:errcheck
	return nil
}
