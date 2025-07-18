package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kairos-io/kairos-sdk/types"
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
	oldLog := mainModel.log
	cc := NewInteractiveInstallConfig(&mainModel)
	// Create a new logger to track the install process output
	// TODO: Maybe do a dual logger or something? So we can still see the output in the old logger decently
	logBuffer := bytes.Buffer{}
	bufferLog := types.NewBufferLogger(&logBuffer)
	cc.Logger = bufferLog

	// Start the installer in a goroutine
	go func() {
		defer close(p.done)
		err := RunInstall(cc)
		if err != nil {
			return
		}
	}()

	// Track the log buffer and send mapped steps to p.output
	go func() {
		lastLen := 0
		for {
			time.Sleep(100 * time.Millisecond)
			buf := logBuffer.Bytes()
			if len(buf) > lastLen {
				newLogs := buf[lastLen:]
				lines := bytes.Split(newLogs, []byte("\n"))
				for _, line := range lines {
					strLine := string(line)
					if len(strLine) == 0 {
						continue
					}

					oldLog.Print(strLine)
					// Parse log line as JSON and extract the message field
					var logEntry map[string]interface{}
					msg := strLine
					if err := json.Unmarshal([]byte(strLine), &logEntry); err == nil {
						// Log the message to the old logger still so we have it there
						if m, ok := logEntry["message"].(string); ok {
							msg = m
						}
					}

					if strings.Contains(msg, AgentPartitionLog) {
						p.output <- StepPrefix + InstallPartitionStep
					} else if strings.Contains(msg, AgentBeforeInstallLog) {
						p.output <- StepPrefix + InstallBeforeInstallStep
					} else if strings.Contains(msg, AgentActiveLog) {
						p.output <- StepPrefix + InstallActiveStep
					} else if strings.Contains(msg, AgentBootloaderLog) {
						p.output <- StepPrefix + InstallBootloaderStep
					} else if strings.Contains(msg, AgentRecoveryLog) {
						p.output <- StepPrefix + InstallRecoveryStep
					} else if strings.Contains(msg, AgentPassiveLog) {
						p.output <- StepPrefix + InstallPassiveStep
					} else if strings.Contains(msg, AgentAfterInstallLog) && !strings.Contains(msg, "chroot") {
						p.output <- StepPrefix + InstallAfterInstallStep
					} else if strings.Contains(msg, AgentCompleteLog) {
						p.output <- StepPrefix + InstallCompleteStep
					}
				}
				lastLen = len(buf)
			}
			select {
			case <-p.done:
				return
			default:
			}
		}
	}()

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
		// Make the warning message red
		warning := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).Render("[!]  Do not power off the system during installation!")
		s += "\n" + warning
	} else {
		// Make the completion message green
		complete := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true).Render("Installation completed successfully!\nYou can now reboot your system.")
		s += "\n" + complete
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
