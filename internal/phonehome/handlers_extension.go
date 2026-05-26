package phonehome

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// execCommand is a seam for tests. Production code path is exec.Command.
var execCommand = exec.Command

type ExtensionArgs struct {
	Type      string
	Action    string
	Name      string
	Source    string
	BootState string
	Now       bool
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

func handleExtension(ctx context.Context, cmd CommandData) (string, error) {
	args, err := parseExtensionArgs(cmd.Args)
	if err != nil {
		return "", err
	}
	switch args.Action {
	case "install":
		return extInstall(ctx, args)
	default:
		return "", fmt.Errorf("extension: action %q not yet implemented", args.Action)
	}
}

// extInstall is install + enable. kairos-agent's `install` subcommand only
// downloads the .raw; `enable` creates the symlink under the chosen scope.
// We do both so AuroraBoot's "Install" action card is one atomic round-trip
// from the operator's view.
func extInstall(ctx context.Context, a ExtensionArgs) (string, error) {
	out1, err := runCLI(ctx, a.Type, "install", a.Source)
	if err != nil {
		return out1, fmt.Errorf("extension install: %w: %s", err, out1)
	}
	enableArgs := []string{a.Type, "enable", a.Name, "--" + a.BootState}
	if a.Now {
		enableArgs = append(enableArgs, "--now")
	}
	out2, err := runCLI(ctx, enableArgs...)
	if err != nil {
		return out1 + "\n" + out2, fmt.Errorf("extension enable: %w: %s", err, out2)
	}
	return fmt.Sprintf("Extension %s installed and enabled in %s\n%s\n%s",
		a.Name, a.BootState, strings.TrimSpace(out1), strings.TrimSpace(out2)), nil
}

func runCLI(ctx context.Context, args ...string) (string, error) {
	_ = ctx
	out, err := execCommand("kairos-agent", args...).CombinedOutput()
	return string(out), err
}
