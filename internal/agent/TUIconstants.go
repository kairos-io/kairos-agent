package agent

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Default to true color palette
	kairosBg         = lipgloss.Color("#03153a") // Deep blue background
	kairosHighlight  = lipgloss.Color("#e56a44") // Orange highlight
	kairosHighlight2 = lipgloss.Color("#d54b11") // Red-orange highlight
	kairosAccent     = lipgloss.Color("#ee5007") // Accent orange
	kairosBorder     = lipgloss.Color("#e56a44") // Use highlight for border
	kairosText       = lipgloss.Color("#ffffff") // White text for contrast
	checkMark        = "✓"
)

func init() {
	// Fallback colors for terminal environments that do not support true color
	term := os.Getenv("TERM")
	if strings.Contains(term, "linux") || strings.Contains(term, "-16color") || term == "dumb" {
		kairosBg = lipgloss.Color("0")         // Black
		kairosText = lipgloss.Color("7")       // White
		kairosHighlight = lipgloss.Color("9")  // Bright Red (for title)
		kairosHighlight2 = lipgloss.Color("1") // Red (for minor alerts or secondary info)
		kairosAccent = lipgloss.Color("5")     // Magenta (or "13" if brighter is OK)
		kairosBorder = lipgloss.Color("9")     // Bright Red (matches highlight)
		checkMark = "*"                        // Use a check mark that works in most terminals
	}
}

const (
	genericNavigationHelp = "↑/k: up • ↓/j: down • enter: select"
	StepPrefix            = "STEP:"
	ErrorPrefix           = "ERROR:"
)

// Installation steps for show
const (
	InstallDefaultStep       = "Preparing installation"
	InstallPartitionStep     = "Partitioning disk"
	InstallBeforeInstallStep = "Running before-install"
	InstallActiveStep        = "Installing Active"
	InstallBootloaderStep    = "Configuring bootloader"
	InstallRecoveryStep      = "Creating Recovery"
	InstallPassiveStep       = "Creating Passive"
	InstallAfterInstallStep  = "Running after-install"
	InstallCompleteStep      = "Installation complete!"
)

// Installation steps to identify installer to UI
const (
	AgentPartitionLog     = "Partitioning device"
	AgentBeforeInstallLog = "Running stage: before-install"
	AgentActiveLog        = "Creating file system image"
	AgentBootloaderLog    = "Installing GRUB"
	AgentRecoveryLog      = "Copying /run/cos/state/cOS/active.img source to /run/cos/recovery/cOS/recovery.img"
	AgentPassiveLog       = "Copying /run/cos/state/cOS/active.img source to /run/cos/state/cOS/passive.img"
	AgentAfterInstallLog  = "Running stage: after-install"
	AgentCompleteLog      = "Installation complete" // This is not reported by the agent, we should add it.
)
