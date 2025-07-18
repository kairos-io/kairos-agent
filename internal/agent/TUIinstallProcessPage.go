package agent

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Install Process Page
type installProcessPage struct {
	progress int
	step     string
	steps    []string
	done     chan bool   // Channel to signal when installation is complete
	output   chan string // Channel to receive output from the installer
	cmd      *exec.Cmd   // Reference to the running installer command
}

func newInstallProcessPage() *installProcessPage {
	return &installProcessPage{
		progress: 0,
		step:     InstallDefaultStep,
		steps: []string{
			InstallDefaultStep,
			InstallPartitionStep,
			InstallBeforeInstallStep,
			InstallActiveStep,
			InstallBootloaderStep,
			InstallRecoveryStep,
			InstallPassiveStep,
			InstallAfterInstallStep,
			InstallCompleteStep,
		},
		done:   make(chan bool),
		output: make(chan string),
	}
}

func (p *installProcessPage) Init() tea.Cmd {
	// Save the configuration before starting the installation
	_ = NewInteractiveInstallConfig(&mainModel)
	// call agent here but this fails due to circular dep

	// TODO: Change this to call the Runinstall function directly
	// Start the actual installer binary as a background process
	/*
		go func() {
			defer close(p.done)

			//agent.RunInstall(c)

			cmd := exec.Command("kairos-agent", "manual-install", filepath.Join(os.TempDir(), "kairos-install-config.yaml"))
			p.cmd = cmd // Store reference to cmd

			// Create pipes for stdout and stderr
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				mainModel.log.Printf("Error creating stdout pipe: %v", err)
				return
			}

			stderr, err := cmd.StderrPipe()
			if err != nil {
				mainModel.log.Printf("Error creating stderr pipe: %v", err)
				return
			}

			// Start the command
			if err := cmd.Start(); err != nil {
				mainModel.log.Printf("Error starting installer: %v", err)
				return
			}

			// Create a scanner to read stdout line by line
			scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))

			// Read output and send it to the channel
			go func() {
				for scanner.Scan() {
					line := scanner.Text()
					mainModel.log.Printf("Installer output: %s", line)

					// Parse output to determine current step based on keywords
					// Basically the output of agent doesnt match exactly what we want to show in the UI,
					// so we map what we found in the agent output to the steps we want to show in the UI.
					if strings.Contains(line, AgentPartitionLog) {
						p.output <- StepPrefix + InstallPartitionStep
					} else if strings.Contains(line, AgentBeforeInstallLog) {
						p.output <- StepPrefix + InstallBeforeInstallStep
					} else if strings.Contains(line, AgentActiveLog) {
						p.output <- StepPrefix + InstallActiveStep
					} else if strings.Contains(line, AgentBootloaderLog) {
						p.output <- StepPrefix + InstallBootloaderStep
					} else if strings.Contains(line, AgentRecoveryLog) {
						p.output <- StepPrefix + InstallRecoveryStep
					} else if strings.Contains(line, AgentPassiveLog) {
						p.output <- StepPrefix + InstallPassiveStep
					} else if strings.Contains(line, AgentAfterInstallLog) && !strings.Contains(line, "chroot") {
						p.output <- StepPrefix + InstallAfterInstallStep
					} else if strings.Contains(line, AgentCompleteLog) {
						p.output <- StepPrefix + InstallCompleteStep
					}
				}
			}()

			// Wait for the command to complete
			if err := cmd.Wait(); err != nil {
				mainModel.log.Printf("Error waiting for installer: %v", err)
				p.output <- ErrorPrefix + err.Error()
			} else {
				mainModel.log.Printf("Installation completed successfully")
				p.output <- StepPrefix + InstallCompleteStep
			}
		}()


	*/
	// Return a command that will check for output from the installer
	return func() tea.Msg {
		return CheckInstallerMsg{}
	}
}

// CheckInstallerMsg Message type to check for installer output
type CheckInstallerMsg struct{}

func (p *installProcessPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg.(type) {
	case CheckInstallerMsg:
		// Check for new output from the installer
		select {
		case output, ok := <-p.output:
			if !ok {
				// Channel closed, installer is done
				return p, nil
			}

			// Process the output
			if strings.HasPrefix(output, StepPrefix) {
				// This is a step change notification
				stepName := strings.TrimPrefix(output, StepPrefix)

				// Find the index of the step
				for i, s := range p.steps {
					if s == stepName {
						p.progress = i
						p.step = stepName
						break
					}
				}
			} else if strings.HasPrefix(output, ErrorPrefix) {
				// Handle error
				errorMsg := strings.TrimPrefix(output, ErrorPrefix)
				p.step = "Error: " + errorMsg
				return p, nil
			}

			// Continue checking for output
			return p, func() tea.Msg { return CheckInstallerMsg{} }

		case <-p.done:
			// Installer is finished
			p.progress = len(p.steps) - 1
			p.step = p.steps[len(p.steps)-1]
			return p, nil

		default:
			// No new output yet, check again after a short delay
			return p, tea.Tick(time.Millisecond*100, func(_ time.Time) tea.Msg {
				return CheckInstallerMsg{}
			})
		}
	}

	return p, nil
}

func (p *installProcessPage) View() string {
	s := "Installation in Progress\n\n"

	// Progress bar
	totalSteps := len(p.steps)
	progressPercent := (p.progress * 100) / (totalSteps - 1)
	barWidth := 40 // Make progress bar wider
	filled := barWidth * progressPercent / 100
	progressBar := lipgloss.NewStyle().Foreground(kairosHighlight2).Background(kairosBg).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(kairosBorder).Background(kairosBg).Render(strings.Repeat("░", barWidth-filled))

	s += "Progress:" + progressBar + lipgloss.NewStyle().Background(kairosBg).Render(" ")
	s += lipgloss.NewStyle().Foreground(kairosText).Background(kairosBg).Bold(true).Render(fmt.Sprintf("%d%%", progressPercent))
	s += "\n\n"
	s += fmt.Sprintf("Current step: %s\n\n", p.step)

	// Show completed steps
	s += "Completed steps:\n"
	tick := lipgloss.NewStyle().Foreground(kairosAccent).Render(checkMark)
	for i := 0; i < p.progress; i++ {
		s += fmt.Sprintf("%s %s\n", tick, p.steps[i])
	}

	if p.progress < len(p.steps)-1 {
		s += "\n[!]  Do not power off the system during installation!"
	} else {
		s += "\nInstallation completed successfully!"
		s += "\nYou can now reboot your system."
	}

	return s
}

func (p *installProcessPage) Title() string {
	return "Installing"
}

func (p *installProcessPage) Help() string {
	if p.progress >= len(p.steps)-1 {
		return "Press any key to exit"
	}
	return "Installation in progress - Use ctrl+c to abort"
}

func (p *installProcessPage) ID() string { return "install_process" }

// Abort aborts the running installer process and cleans up
func (p *installProcessPage) Abort() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		mainModel.log.Info("Installer process aborted by user")
	}
	// Close output channel if not already closed
	select {
	case <-p.done:
		// already closed
	default:
		close(p.done)
	}
	// Optionally, send a message to output channel
	select {
	case p.output <- ErrorPrefix + "Installation aborted by user":
	default:
	}
}
