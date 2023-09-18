package uki

import (
	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	elementalUtils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	events "github.com/kairos-io/kairos-sdk/bus"
)

type InstallAction struct {
	cfg  *config.Config
	spec *v1.InstallUkiSpec
}

func NewInstallAction(cfg *config.Config, spec *v1.InstallUkiSpec) *InstallAction {
	return &InstallAction{cfg: cfg, spec: spec}
}

func (i *InstallAction) Run() (err error) {
	// Run pre-install stage
	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-install.pre")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.install.pre.hook")

	// Get source (from spec?)
	// Create EFI partition (fat32), we already create the efi partition on normal efi install,w e can reuse that?
	// Create COS_OEM/COS_PERSISTANT if set (optional)
	// I guess we need to set sensible default values here for sizes? oem -> 64Mb as usual but if no persistent then EFI max size?
	// if persistent then EFI = source size * 2 (or maybe 3 times! so we can upgrade!) and then persistent the rest of the disk?
	// Store cloud-config in TPM or copy it to COS_OEM?
	// Create dir structure
	//  - /EFI/Kairos/ -> Store our efi images
	//  - /EFI/BOOT/ -> Default fallback dir (efi search for bootaa64.efi or bootx64.efi if no entries in the boot manager)
	// NOTE: Maybe softlink fallback to kairos? Not sure if that will work
	// Copy the efi file into the proper dir
	// Remove all boot manager entries?
	// Create boot manager entry
	// Set default entry to the one we just created
	// Probably copy efi utils, like the Mokmanager and even the shim or grub efi to help with troubleshooting?

	_ = elementalUtils.RunStage(i.cfg, "kairos-uki-install.after")
	_ = events.RunHookScript("/usr/bin/kairos-agent.uki.install.after.hook") //nolint:errcheck

	return hook.Run(*i.cfg, i.spec, hook.AfterUkiInstall...)
}
