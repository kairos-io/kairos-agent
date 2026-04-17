package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
)

const phoneHomeServiceName = "kairos-agent-phonehome"
const phoneHomeServicePath = "/etc/systemd/system/kairos-agent-phonehome.service"
const phoneHomeServiceContent = `[Unit]
Description=Kairos Agent Phone Home
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/sbin/kairos-agent phone-home
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`

// enablePhoneHomeIfConfigured installs and starts the phone-home systemd service
// when a `phonehome:` section with a url is present in the merged cloud-config.
// The config is parsed by phonehome.LoadFromCollector so it uses the same
// Collector output as the rest of the kairos-agent (no separate file walk).
func enablePhoneHomeIfConfigured(c *sdkConfig.Config) {
	cfg, ok, err := phonehome.LoadFromCollector(c)
	if err != nil {
		fmt.Printf("Warning: could not parse phonehome config: %v\n", err)
		return
	}
	if !ok || cfg.URL == "" {
		return
	}

	fmt.Println("Phone-home configuration detected, enabling phone-home service")

	if err := os.WriteFile(phoneHomeServicePath, []byte(phoneHomeServiceContent), 0600); err != nil {
		fmt.Printf("Warning: failed to write phone-home service: %v\n", err)
		return
	}

	for _, args := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", phoneHomeServiceName},
		{"systemctl", "start", phoneHomeServiceName},
	} {
		// args is a literal slice defined just above — no user input reaches exec.Command.
		cmd := exec.Command(args[0], args[1:]...) //nosec G204 -- fixed command and arguments
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Warning: %s failed: %v (%s)\n", strings.Join(args, " "), err, string(out))
		}
	}
}
