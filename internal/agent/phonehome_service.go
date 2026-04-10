package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const phoneHomeServiceName = "kairos-agent-phonehome"
const phoneHomeServicePath = "/etc/systemd/system/kairos-agent-phonehome.service"
const phoneHomeServiceContent = `[Unit]
Description=Kairos Agent Phone Home (Daedalus)
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

// enablePhoneHomeIfConfigured checks if daedalus configuration exists in the
// cloud-config directories and, if so, installs and starts the phone-home
// systemd service. This is called during `kairos-agent start`.
func enablePhoneHomeIfConfigured(dirs []string) {
	if !hasDaedalusConfig(dirs) {
		return
	}

	fmt.Println("Daedalus configuration detected, enabling phone-home service")

	// Write the systemd service file
	if err := os.WriteFile(phoneHomeServicePath, []byte(phoneHomeServiceContent), 0644); err != nil {
		fmt.Printf("Warning: failed to write phone-home service: %v\n", err)
		return
	}

	// Reload systemd and enable + start the service
	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", phoneHomeServiceName},
		{"systemctl", "start", phoneHomeServiceName},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Warning: %s failed: %v (%s)\n", strings.Join(args, " "), err, string(out))
		}
	}
}

// hasDaedalusConfig checks if any cloud-config file in the given directories
// contains a `daedalus:` section with a `url` field.
func hasDaedalusConfig(dirs []string) bool {
	for _, dir := range dirs {
		found := false
		// Walk recursively — Kairos stores datasource cloud-config in
		// subdirectories like /oem/95_userdata/userdata.yaml
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".yaml") && !strings.HasSuffix(info.Name(), ".yml") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			var cc struct {
				Daedalus struct {
					URL string `yaml:"url"`
				} `yaml:"daedalus"`
			}
			if yaml.Unmarshal(data, &cc) == nil && cc.Daedalus.URL != "" {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}
