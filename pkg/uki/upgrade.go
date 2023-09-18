package uki

import (
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
)

type UpgradeAction struct {
	cfg  *config.Config
	spec *v1.EmptySpec
}

func NewUpgradeAction(cfg *config.Config, spec *v1.EmptySpec) *UpgradeAction {
	return &UpgradeAction{cfg: cfg, spec: spec}
}

func (i *UpgradeAction) Run() (err error) {
	// Run pre-install stage
	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.pre.hook")

	// Get source (from spec?)
	// Copy the efi file into the proper dir
	// Remove all boot manager entries?
	// Create boot manager entry
	// Set default entry to the one we just created

	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-upgrade.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.upgrade.after.hook") //nolint:errcheck

	return hook.Run(*i.cfg, i.spec, hook.AfterUkiUpgrade...)
}
