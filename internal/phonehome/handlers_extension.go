package phonehome

import (
	"context"
	"fmt"
)

// ExtensionArgs is the validated, typed shape of an `extension` command's args.
type ExtensionArgs struct {
	Type      string // "sysext" | "confext"
	Action    string // "install" | "enable" | "disable" | "remove"
	Name      string
	Source    string // required for action=install
	BootState string // required for action in {install,enable,disable}
	Now       bool   // optional
}

func parseExtensionArgs(in map[string]string) (ExtensionArgs, error) {
	out := ExtensionArgs{
		Type:      in["type"],
		Action:    in["action"],
		Name:      in["name"],
		Source:    in["source"],
		BootState: in["bootState"],
		Now:       in["now"] == "true",
	}
	if out.Type != "sysext" && out.Type != "confext" {
		return out, fmt.Errorf("extension: unsupported type %q (want sysext or confext)", out.Type)
	}
	switch out.Action {
	case "install", "enable", "disable", "remove":
	default:
		return out, fmt.Errorf("extension: unsupported action %q (want install|enable|disable|remove)", out.Action)
	}
	if out.Name == "" {
		return out, fmt.Errorf("extension: name is required")
	}
	if out.Action == "install" && out.Source == "" {
		return out, fmt.Errorf("extension: source is required for action=install")
	}
	if (out.Action == "install" || out.Action == "enable" || out.Action == "disable") && out.BootState == "" {
		return out, fmt.Errorf("extension: bootState is required for action=%s", out.Action)
	}
	switch out.BootState {
	case "", "active", "passive", "recovery", "common":
	default:
		return out, fmt.Errorf("extension: unsupported bootState %q", out.BootState)
	}
	return out, nil
}

// handleExtension dispatches the manual-flow extension command. The stub
// returned here is replaced in subsequent tasks with the install/enable/
// disable/remove action implementations.
func handleExtension(ctx context.Context, cmd CommandData) (string, error) {
	args, err := parseExtensionArgs(cmd.Args)
	if err != nil {
		return "", err
	}
	_ = ctx
	return "", fmt.Errorf("extension: action %q not yet implemented", args.Action)
}
