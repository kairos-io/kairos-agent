package phonehome

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
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
			return "reset not yet implemented", nil

		case "apply-cloud-config":
			return "apply-cloud-config not yet implemented", nil

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

	out, err := exec.CommandContext(ctx, "kairos-agent", args...).CombinedOutput()
	return string(out), err
}
