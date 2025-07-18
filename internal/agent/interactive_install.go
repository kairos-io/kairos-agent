package agent

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/kairos-io/kairos-sdk/types"
	"os"

	"github.com/kairos-io/kairos-sdk/utils"
)

// InteractiveInstall starts the interactive installation process.
// The function signature was updated to replace the `debug` parameter with a `logger` parameter (`l types.KairosLogger`).
// - `spawnShell`: If true, spawns a shell after the installation process.
// - `source`: The source of the installation. (Consider reviewing its necessity as noted in the TODO comment.)
// - `l`: A logger instance for logging messages during the installation process.
func InteractiveInstall(spawnShell bool, source string, l types.KairosLogger) error {
	var err error
	// Set a default window size
	p := tea.NewProgram(InitialModel(&l, source), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
	//TODO: This will always exit and return I think, so the below is useless? Unless we want to hijack the TTY in which case we should do something here for that
	if spawnShell {
		return utils.Shell().Run()
	}
	return err
}
