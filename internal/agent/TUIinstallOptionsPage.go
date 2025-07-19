package agent

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Install Options Page
type installOptionsPage struct {
	cursor  int
	options []string
}

func newInstallOptionsPage() *installOptionsPage {
	return &installOptionsPage{
		options: []string{
			"Start Install",
			"Customize Further (User, SSH Keys, etc.)",
		},
		cursor: 0,
	}
}

func (p *installOptionsPage) Init() tea.Cmd {
	return nil
}

func (p *installOptionsPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.options)-1 {
				p.cursor++
			}
		case "enter":
			if p.cursor == 0 {
				// Start Install - go to install process
				return p, func() tea.Msg { return GoToPageMsg{PageID: "summary"} }
			} else {
				// Customize Further - go to customization page
				return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
			}
		}
	}
	return p, nil
}

func (p *installOptionsPage) View() string {
	s := "Installation Options\n\n"
	s += "Choose how to proceed:\n\n"

	for i, option := range p.options {
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		s += fmt.Sprintf("%s %s\n", cursor, option)
	}

	return s
}

func (p *installOptionsPage) Title() string {
	return "Install Options"
}

func (p *installOptionsPage) Help() string {
	return genericNavigationHelp
}

func (p *installOptionsPage) ID() string { return "install_options" }
