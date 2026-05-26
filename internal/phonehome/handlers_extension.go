package phonehome

import (
	"context"
	"encoding/json"
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
	case "enable":
		return extToggle(ctx, args, "enable")
	case "disable":
		return extToggle(ctx, args, "disable")
	case "remove":
		return extRemove(ctx, args)
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

func extToggle(ctx context.Context, a ExtensionArgs, action string) (string, error) {
	cliArgs := []string{a.Type, action, a.Name, "--" + a.BootState}
	if a.Now {
		cliArgs = append(cliArgs, "--now")
	}
	out, err := runCLI(ctx, cliArgs...)
	if err != nil {
		return out, fmt.Errorf("extension %s: %w: %s", action, err, out)
	}
	return strings.TrimSpace(out), nil
}

func extRemove(ctx context.Context, a ExtensionArgs) (string, error) {
	cliArgs := []string{a.Type, "remove", a.Name}
	if a.Now {
		cliArgs = append(cliArgs, "--now")
	}
	out, err := runCLI(ctx, cliArgs...)
	if err != nil {
		return out, fmt.Errorf("extension remove: %w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

func runCLI(ctx context.Context, args ...string) (string, error) {
	_ = ctx
	out, err := execCommand("kairos-agent", args...).CombinedOutput()
	return string(out), err
}

// BundledExtension is one entry inside the upgrade command's `extensions` arg.
// The on-wire shape is a JSON-encoded array passed as a string under
// CommandData.Args["extensions"] because Args is map[string]string.
type BundledExtension struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

func parseBundledExtensions(raw string) ([]BundledExtension, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var list []BundledExtension
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("extensions arg: %w", err)
	}
	for i, e := range list {
		if e.Type != "sysext" && e.Type != "confext" {
			return nil, fmt.Errorf("extensions[%d]: unsupported type %q", i, e.Type)
		}
		if e.Name == "" {
			return nil, fmt.Errorf("extensions[%d]: name is required", i)
		}
		if e.Source == "" {
			return nil, fmt.Errorf("extensions[%d]: source is required", i)
		}
	}
	return list, nil
}
