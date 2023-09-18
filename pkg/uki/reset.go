package uki

import (
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
)

type ResetAction struct {
	cfg  *config.Config
	spec *v1.EmptySpec
}

func NewResetAction(cfg *config.Config, spec *v1.EmptySpec) *ResetAction {
	return &ResetAction{cfg: cfg, spec: spec}
}

func (i *ResetAction) Run() (err error) {
	// Run pre-install stage
	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-reset.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.reset.pre.hook")

	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-reset.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.reset.after.hook") //nolint:errcheck

	return hook.Run(*i.cfg, i.spec, hook.AfterUkiReset...)
}
