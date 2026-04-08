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
)

// DefaultCommandHandler returns a CommandHandler that handles all daedalus commands.
// The daedalusURL and apiKey are needed to download artifact images for upgrades.
func DefaultCommandHandler(daedalusURL string, apiKey func() string) CommandHandler {
	return func(cmd CommandData) (string, error) {
		ctx := context.Background()

		switch cmd.Command {
		case "exec":
			cmdStr, ok := cmd.Args["command"]
			if !ok {
				return "", fmt.Errorf("exec command requires 'command' arg")
			}
			out, err := exec.CommandContext(ctx, "sh", "-c", cmdStr).CombinedOutput()
			return string(out), err

		case "upgrade", "upgrade-recovery":
			return handleUpgrade(ctx, cmd, daedalusURL, apiKey())

		case "reset":
			return handleReset(cmd)

		case "apply-cloud-config":
			return handleApplyCloudConfig(cmd)

		case "reboot":
			return handleReboot()

		default:
			return "", fmt.Errorf("unknown command: %s", cmd.Command)
		}
	}
}

// handleUpgrade downloads the image (if artifact-based) and runs kairos-agent upgrade.
func handleUpgrade(ctx context.Context, cmd CommandData, daedalusURL string, apiKey string) (string, error) {
	source := cmd.Args["source"]
	if source == "" {
		return "", fmt.Errorf("upgrade requires 'source' arg")
	}

	// If source is "artifact:<id>", download the container image tar from daedalus
	if strings.HasPrefix(source, "artifact:") {
		artifactID := strings.TrimPrefix(source, "artifact:")
		tarPath := fmt.Sprintf("/tmp/daedalus-upgrade-%s.tar", artifactID)

		imageURL := fmt.Sprintf("%s/api/v1/artifacts/%s/image?token=%s",
			strings.TrimRight(daedalusURL, "/"), artifactID, apiKey)

		resp, err := http.Get(imageURL)
		if err != nil {
			return "", fmt.Errorf("downloading artifact image: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("downloading artifact image: HTTP %d", resp.StatusCode)
		}

		f, err := os.Create(tarPath)
		if err != nil {
			return "", fmt.Errorf("creating tar file: %w", err)
		}
		if _, err = io.Copy(f, resp.Body); err != nil {
			f.Close()
			os.Remove(tarPath)
			return "", fmt.Errorf("writing tar file: %w", err)
		}
		f.Close()
		defer os.Remove(tarPath)

		source = "ocifile:" + tarPath
	} else if !strings.Contains(source, ":") {
		source = "oci:" + source
	}

	args := []string{"upgrade", "--source", source}
	if cmd.Command == "upgrade-recovery" || cmd.Args["recovery"] == "true" {
		args = append(args, "--recovery")
	}

	// Use background context — upgrade must NOT be killed if WS disconnects
	fmt.Printf("[phonehome] Running: kairos-agent %s\n", strings.Join(args, " "))
	out, err := exec.Command("kairos-agent", args...).CombinedOutput()
	fmt.Printf("[phonehome] Exit: err=%v output=%s\n", err, string(out))
	if err != nil {
		return string(out), err
	}

	// Reboot after successful upgrade so the new image takes effect.
	// Do NOT reboot for recovery upgrades (recovery doesn't need reboot).
	if cmd.Command != "upgrade-recovery" {
		scheduleReboot()
	}

	return string(out) + "\nUpgrade complete. Rebooting in 10s...", nil
}

// handleReset runs kairos-agent reset and optionally writes a cloud-config after.
func handleReset(cmd CommandData) (string, error) {
	args := []string{"reset", "--unattended"}
	if cmd.Args["reset-oem"] == "true" {
		args = append(args, "--reset-oem")
	}
	if cmd.Args["reset-persistent"] == "true" {
		args = append(args, "--reset-persistent")
	}

	fmt.Printf("[phonehome] Running: kairos-agent %s\n", strings.Join(args, " "))
	out, err := exec.Command("kairos-agent", args...).CombinedOutput()
	fmt.Printf("[phonehome] Exit: err=%v output=%s\n", err, string(out))
	if err != nil {
		return string(out), err
	}

	// If a cloud-config was provided, write it to OEM after reset.
	// OEM may have been wiped (--reset-oem) so we remount it first.
	if cfg := cmd.Args["config"]; cfg != "" {
		if err := writeOEMCloudConfig(cfg); err != nil {
			return string(out) + "\nReset succeeded but failed to write cloud config: " + err.Error(), err
		}
	}

	scheduleReboot()
	return string(out) + "\nReset complete. Rebooting in 10s...", nil
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

	return "Cloud config written to /oem/99_daedalus_remote.yaml. Reboot to apply.", nil
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

	// Ensure /oem is mounted (it may have been unmounted during reset)
	os.MkdirAll("/oem", 0755)
	// Try to mount — ignore error if already mounted
	exec.Command("mount", "-L", "COS_OEM", "/oem").Run()

	return os.WriteFile("/oem/99_daedalus_remote.yaml", []byte(content), 0644)
}

// scheduleReboot syncs filesystems and reboots after a short delay.
func scheduleReboot() {
	go func() {
		exec.Command("sync").Run()
		time.Sleep(10 * time.Second)
		exec.Command("reboot").Run()
	}()
}
