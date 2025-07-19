package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/option"
)

type diskStruct struct {
	id   int
	name string
	size string
}

// Disk Selection Page
type diskSelectionPage struct {
	disks  []diskStruct
	cursor int
}

func newDiskSelectionPage() *diskSelectionPage {
	bl, err := block.New(option.WithDisableTools(), option.WithNullAlerter())
	if err != nil {
		fmt.Printf("Error initializing block device info: %v\n", err)
		return nil
	}
	var disks []diskStruct

	const minDiskSizeBytes = 1 * 1024 * 1024 * 1024 // 1 GiB
	excludedDevicePrefixes := []string{"loop", "ram", "sr", "zram"}

	for _, disk := range bl.Disks {
		// Check if the device name starts with any excluded prefix
		excluded := false
		for _, prefix := range excludedDevicePrefixes {
			if strings.HasPrefix(disk.Name, prefix) {
				excluded = true
				break
			}
		}
		if excluded || disk.SizeBytes < minDiskSizeBytes {
			continue // Skip excluded devices and disks smaller than the minimum size
		}
		disks = append(disks, diskStruct{name: filepath.Join("/dev", disk.Name), size: fmt.Sprintf("%.2f GiB", float64(disk.SizeBytes)/float64(1024*1024*1024)), id: len(disks)})
	}

	return &diskSelectionPage{
		disks:  disks,
		cursor: 0,
	}
}

func (p *diskSelectionPage) Init() tea.Cmd {
	return nil
}

func (p *diskSelectionPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.disks)-1 {
				p.cursor++
			}
		case "enter":
			// Store selected disk in mainModel
			if p.cursor >= 0 && p.cursor < len(p.disks) {
				mainModel.disk = p.disks[p.cursor].name
			}
			// Go to confirmation page
			return p, func() tea.Msg { return GoToPageMsg{PageID: "install_options"} }
		}
	}
	return p, nil
}

func (p *diskSelectionPage) View() string {
	s := "Select target disk for installation:\n\n"
	s += "WARNING: All data on the selected disk will be DESTROYED!\n\n"

	for i, disk := range p.disks {
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		s += fmt.Sprintf("%s %s (%s)\n", cursor, disk.name, disk.size)
	}

	return s
}

func (p *diskSelectionPage) Title() string {
	return "Disk Selection"
}

func (p *diskSelectionPage) Help() string {
	return genericNavigationHelp
}

func (p *diskSelectionPage) ID() string { return "disk_selection" }
