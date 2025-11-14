package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

// Install Process Page
type installProcessPage struct {
	progress      int
	step          string
	steps         []string
	done          chan bool
	output        chan string
	logsDone      chan bool
	installerDone chan bool // Signal when installer is finished
	once          sync.Once // Ensure goroutines are started only once
	cmd           *exec.Cmd
	errorMsg      string
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
		done:          make(chan bool),
		output:        make(chan string, 10), // Buffered channel to avoid blocking
		logsDone:      make(chan bool),
		installerDone: make(chan bool),
	}
}

func (p *installProcessPage) Init() tea.Cmd {
	p.once.Do(func() {
		oldLog := mainModel.log
		cc := NewInteractiveInstallConfig(&mainModel)
		if cc == nil {
			p.errorMsg = "Failed to initialize install configuration."
			return
		}
		logBuffer := bytes.Buffer{}
		bufferLog := sdkLogger.NewBufferLogger(&logBuffer)
		cc.Logger = bufferLog

		// Start log goroutine (only one!)
		go func() {
			lastLen := 0
			errorSent := false
		logLoop:
			for {
				time.Sleep(100 * time.Millisecond)
				buf := logBuffer.Bytes()
				if len(buf) > lastLen {
					lastLen = p.processLogLines(buf, lastLen, &errorSent, oldLog)
				}
				// Wait for installerDone before exiting
				select {
				case <-p.installerDone:
					// Installer is done, but there may be unprocessed logs
					for {
						buf := logBuffer.Bytes()
						if len(buf) > lastLen {
							lastLen = p.processLogLines(buf, lastLen, &errorSent, oldLog)
						} else {
							break
						}
					}
					break logLoop
				default:
				}
			}
			close(p.logsDone) // Signal that log goroutine is done
		}()

		// Start installer goroutine (only one!)
		go func() {
			err := RunInstall(cc)
			close(p.installerDone) // Signal installer is done
			<-p.logsDone           // Wait for logs to finish
			close(p.output)        // Only close output here
			close(p.done)          // Only close done here
			_ = err                // ignore error, handled in logs
		}()
	})

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
				p.step = "Error: " + errorMsg // This stops the progress bar from continuing
				p.errorMsg = errorMsg
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

	if p.errorMsg != "" {
		s := "Installation encountered an error.\n\n"
		// Show error message in red
		errMsgStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).Render("[!] Installation error: " + p.errorMsg)
		s += errMsgStyled + "\n\n"
		return s
	}

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
		text := "Installation completed successfully!\n"
		if mainModel.finishAction == "nothing" {
			text += "You can now reboot or shut down your system."
		}
		complete := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true).Render(text)
		s += "\n" + complete
	}

	return s
}

func (p *installProcessPage) Title() string {
	return "Installing"
}

func (p *installProcessPage) Help() string {
	if p.progress >= len(p.steps)-1 || p.errorMsg != "" {
		if mainModel.finishAction == "nothing" {
			return "Press any key to exit"
		} else {
			return "System will " + mainModel.finishAction + " shortly"
		}

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
	}
}

// processLogLines processes new log lines from the buffer and updates the UI steps.
func (p *installProcessPage) processLogLines(buf []byte, lastLen int, errorSent *bool, oldLog *sdkLogger.KairosLogger) int {
	newLogs := buf[lastLen:]
	lines := bytes.Split(newLogs, []byte("\n"))
	for _, line := range lines {
		strLine := string(line)
		if len(strLine) == 0 {
			continue
		}
		oldLog.Print(strLine)
		var logEntry map[string]interface{}
		msg := strLine
		if err := json.Unmarshal([]byte(strLine), &logEntry); err == nil {
			if m, ok := logEntry["message"].(string); ok {
				msg = m
			}
			if level, ok := logEntry["level"].(string); ok && (level == "error" || level == "fatal") {
				if !*errorSent {
					p.errorMsg = msg
					*errorSent = true
				}
				continue
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
		} else if strings.Contains(msg, AgentStartLifecycle) && mainModel.finishAction != "nothing" {
			// Lifecycle start can be considered as finished if we are rebooting or shutting down as that hook
			// will reboot/shutdown the system and not return any log after that
			p.output <- StepPrefix + InstallCompleteStep
		} else if strings.Contains(msg, AgentCompleteLog) {
			p.output <- StepPrefix + InstallCompleteStep
		}
	}
	return len(buf)
}
