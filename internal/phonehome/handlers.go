package phonehome

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
)

var selectBootEntry = action.SelectBootEntry
var rebootScheduler = scheduleReboot

// DefaultCommandHandler returns a CommandHandler that handles all phone-home commands.
// The serverURL and apiKey are needed to download artifact images for upgrades.
//
// isAllowed gates execution: a command is only dispatched if isAllowed(cmd.Command)
// returns true. This exists because a rogue/DNS-hijacked server could otherwise
// drive arbitrary `exec`, `reset`, or `apply-cloud-config` on the node. Destructive
// commands are opt-in per Config.AllowedCommands. If isAllowed is nil, every command
// is denied (safer default than allowing everything when the caller forgets to wire
// the policy through).
//
// stop is invoked after a successful `unregister` teardown so the long-lived
// phonehome Run loop stops reconnecting. It is nil-safe (nil => no self-exit,
// useful for tests and one-shot handler drivers); in production the Client
// passes its own Stop method in.
func DefaultCommandHandler(serverURL string, apiKey func() string, isAllowed func(string) bool, stop func(), systemConfig *sdkConfig.Config) CommandHandler {
	return func(cmd CommandData) (string, error) {
		if isAllowed == nil || !isAllowed(cmd.Command) {
			return "", fmt.Errorf("command %q is not permitted by the phonehome policy; add it to phonehome.allowed_commands in cloud-config to opt in", cmd.Command)
		}

		ctx := context.Background()

		switch cmd.Command {
		case "exec":
			cmdStr, ok := cmd.Args["command"]
			if !ok {
				return "", fmt.Errorf("exec command requires 'command' arg")
			}
			// Arbitrary shell is opt-in via phonehome.allowed_commands (see gate above).
			out, err := exec.CommandContext(ctx, "sh", "-c", cmdStr).CombinedOutput() //nosec G204 -- gated by Config.AllowedCommands policy
			return string(out), err

		case "upgrade", "upgrade-recovery":
			return handleUpgrade(ctx, cmd, serverURL, apiKey())

		case "reset":
			return handleReset(cmd, systemConfig)

		case "apply-cloud-config":
			return handleApplyCloudConfig(cmd)

		case "reboot":
			return handleReboot()

		case "unregister":
			return handleUnregister(stop)

		case "extension":
			return handleExtension(ctx, cmd)

		default:
			return "", fmt.Errorf("unknown command: %s", cmd.Command)
		}
	}
}

// handleUnregister runs the on-host phone-home teardown from inside the
// running service and then schedules the Run loop to exit.
//
// Crucially, this passes stopService=false to Uninstall: the handler IS the
// service process, so `systemctl stop kairos-agent-phonehome` would SIGTERM
// it mid-call and the "Completed" WebSocket status would never be written.
// Instead we remove the unit file, daemon-reload, clean up the state on
// disk, and then after the summary is sent the client stops itself via
// Client.Stop(). The 500 ms delay gives the Completed write time to flush
// through the WS and the TCP buffers before we tear down the client.
func handleUnregister(stop func()) (string, error) {
	summary, err := Uninstall(false)
	if stop != nil {
		time.AfterFunc(500*time.Millisecond, stop)
	}
	if err != nil {
		return summary, err
	}
	return summary, nil
}

// handleUpgrade downloads the image (if artifact-based) and runs kairos-agent upgrade.
func handleUpgrade(ctx context.Context, cmd CommandData, serverURL string, apiKey string) (string, error) {
	source := cmd.Args["source"]
	if source == "" {
		return "", fmt.Errorf("upgrade requires 'source' arg")
	}

	// If source is "artifact:<id>", download the container image tar from the server.
	if strings.HasPrefix(source, "artifact:") {
		artifactID := strings.TrimPrefix(source, "artifact:")
		// Artifact IDs come from the management server — constrain to a safe
		// character set so they can't traverse out of /tmp or poison the URL path.
		if !isSafeArtifactID(artifactID) {
			return "", fmt.Errorf("invalid artifact id %q", artifactID)
		}
		tarPath := fmt.Sprintf("/tmp/phonehome-upgrade-%s.tar", artifactID)

		imageURL := fmt.Sprintf("%s/api/v1/artifacts/%s/image?token=%s",
			strings.TrimRight(serverURL, "/"), artifactID, apiKey)

		// serverURL is operator-configured via cloud-config, not user input.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil) //nosec G107 -- URL derived from operator cloud-config
		if err != nil {
			return "", fmt.Errorf("building artifact image request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("downloading artifact image: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("downloading artifact image: HTTP %d", resp.StatusCode)
		}

		f, err := os.Create(tarPath) //nosec G304 -- tarPath is built from a validated artifactID under /tmp
		if err != nil {
			return "", fmt.Errorf("creating tar file: %w", err)
		}
		if _, err = io.Copy(f, resp.Body); err != nil {
			_ = f.Close()
			_ = os.Remove(tarPath)
			return "", fmt.Errorf("writing tar file: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tarPath)
			return "", fmt.Errorf("closing tar file: %w", err)
		}
		defer func() { _ = os.Remove(tarPath) }()

		source = "ocifile:" + tarPath
	} else if !strings.Contains(source, ":") {
		source = "oci:" + source
	}

	args := []string{"upgrade", "--source", source}
	if cmd.Command == "upgrade-recovery" || cmd.Args["recovery"] == "true" {
		args = append(args, "--recovery")
	}

	// Install bundled extensions before the OS upgrade. Each install is
	// idempotent (kairos-agent install overwrites the .raw in place), so a
	// retry of the same compound command after a partial failure is safe.
	bundled, err := parseBundledExtensions(cmd.Args["extensions"])
	if err != nil {
		return "", err
	}
	scope := "active"
	if cmd.Command == "upgrade-recovery" {
		scope = "recovery"
	}
	for _, e := range bundled {
		if err := installBundledExtension(ctx, e, scope); err != nil {
			// Do NOT proceed to the OS upgrade if any extension fails.
			return "", err
		}
	}

	// Use background context — upgrade must NOT be killed if WS disconnects
	Logger.Infof("running: kairos-agent %s", strings.Join(args, " "))
	out, err := execCommand("kairos-agent", args...).CombinedOutput() //nosec G204 -- args is a fixed set built from validated CommandData fields
	if err != nil {
		Logger.Errorf("kairos-agent upgrade exit: err=%v output=%s", err, string(out))
		return string(out), err
	}
	Logger.Infof("kairos-agent upgrade completed: %s", string(out))

	// Reboot after successful upgrade so the new image takes effect.
	// Do NOT reboot for recovery upgrades (recovery doesn't need reboot).
	if cmd.Command != "upgrade-recovery" {
		scheduleReboot()
	}

	return string(out) + "\nUpgrade complete. Rebooting in 10s...", nil
}

// handleReset selects the automatic state-reset boot entry and reboots. Reset
// itself cannot run from the active system; the statereset entry performs it on
// the next boot and then returns the node to the active entry.
func handleReset(cmd CommandData, systemConfig *sdkConfig.Config) (string, error) {
	for _, argument := range []string{"reset-oem", "config"} {
		if _, ok := cmd.Args[argument]; ok {
			return "", fmt.Errorf("reset argument %q is not supported by automatic state reset", argument)
		}
	}
	if systemConfig == nil {
		return "", fmt.Errorf("reset requires the scanned system configuration")
	}
	if err := selectBootEntry(systemConfig, constants.StateResetImgName); err != nil {
		return "", fmt.Errorf("selecting automatic state-reset boot entry: %w", err)
	}

	rebootScheduler()
	return "Automatic state reset selected. Rebooting in 10s...", nil
}

// handleApplyCloudConfig writes a cloud-config file to the OEM partition.
func handleApplyCloudConfig(cmd CommandData) (string, error) {
	cfg := cmd.Args["config"]
	if cfg == "" {
		return "", fmt.Errorf("apply-cloud-config requires 'config' arg")
	}

	if err := writeOEMCloudConfig(cfg); err != nil {
		return "", err
	}

	return "Cloud config written to /oem/99_phonehome_remote.yaml. Reboot to apply.", nil
}

// handleReboot schedules a system reboot.
func handleReboot() (string, error) {
	scheduleReboot()
	return "Rebooting in 10s...", nil
}

// writeOEMCloudConfig ensures OEM is mounted and writes a cloud-config file.
func writeOEMCloudConfig(content string) error {
	// Ensure #cloud-config header
	if !strings.HasPrefix(strings.TrimSpace(content), "#cloud-config") {
		content = "#cloud-config\n" + content
	}

	// Ensure /oem is mounted (it may have been unmounted during reset).
	// MkdirAll is best-effort: if /oem already exists we proceed; any other
	// failure will surface from the mount attempt or WriteFile below.
	if err := os.MkdirAll("/oem", 0750); err != nil {
		Logger.Warnf("mkdir /oem: %v", err)
	}
	// Best-effort mount — error is expected and ignored when /oem is already mounted.
	_ = exec.Command("mount", "-L", "COS_OEM", "/oem").Run() //nosec G204 -- fixed label, called on local mountpoint

	return os.WriteFile("/oem/99_phonehome_remote.yaml", []byte(content), 0600)
}

// scheduleRebootFn is a seam for tests to assert "no reboot scheduled on
// failure". Production points at scheduleRebootImpl.
var scheduleRebootFn = scheduleRebootImpl

// scheduleReboot syncs filesystems and reboots after a short delay.
func scheduleReboot() {
	scheduleRebootFn()
}

func scheduleRebootImpl() {
	go func() {
		// Best-effort flush then reboot — we're going down either way.
		_ = exec.Command("sync").Run() //nosec G204 -- fixed command
		time.Sleep(10 * time.Second)
		_ = exec.Command("reboot").Run() //nosec G204 -- fixed command
	}()
}

// isSafeArtifactID whitelists characters acceptable inside an artifact
// identifier: alphanumeric, dash, underscore, dot. This prevents the ID from
// containing path separators or shell metacharacters when it is interpolated
// into /tmp paths and request URLs.
func isSafeArtifactID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
