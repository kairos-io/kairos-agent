package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/kairos-sdk/types"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var internalLogger = types.NewKairosLogger("interactive-installer", "debug", true)

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
		internalLogger.Logger.Info().Msg("[Init] Starting installer and log goroutines")
		oldLog := mainModel.log
		cc := NewInteractiveInstallConfig(&mainModel)
		if cc == nil {
			internalLogger.Logger.Error().Msg("[Init] Failed to initialize install configuration.")
			p.errorMsg = "Failed to initialize install configuration."
			return
		}
		logBuffer := bytes.Buffer{}
		bufferLog := types.NewBufferLogger(&logBuffer)
		cc.Logger = bufferLog

		// Start log goroutine (only one!)
		go func() {
			internalLogger.Logger.Info().Msg("[Log Goroutine] Started")
			lastLen := 0
			errorSent := false
		logLoop:
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
						internalLogger.Logger.Debug().Msg("[Log Goroutine] Read line: " + strLine)
						var logEntry map[string]interface{}
						msg := strLine
						if err := json.Unmarshal([]byte(strLine), &logEntry); err == nil {
							if m, ok := logEntry["message"].(string); ok {
								msg = m
							}
							if level, ok := logEntry["level"].(string); ok && (level == "error" || level == "fatal") {
								internalLogger.Logger.Info().Msg("[Log Goroutine] Error detected: " + msg)
								if !errorSent {
									p.errorMsg = msg
									select {
									case p.output <- ErrorPrefix + msg:
										internalLogger.Logger.Info().Msg("[Log Goroutine] Sent error to output channel")
									default:
										internalLogger.Logger.Warn().Msg("[Log Goroutine] Output channel blocked, error not sent")
									}
									errorSent = true
								}
								continue
							}
						}
						if strings.Contains(msg, AgentPartitionLog) {
							select {
							case p.output <- StepPrefix + InstallPartitionStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step PartitionStep")
							default:
							}
						} else if strings.Contains(msg, AgentBeforeInstallLog) {
							select {
							case p.output <- StepPrefix + InstallBeforeInstallStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step BeforeInstallStep")
							default:
							}
						} else if strings.Contains(msg, AgentActiveLog) {
							select {
							case p.output <- StepPrefix + InstallActiveStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step ActiveStep")
							default:
							}
						} else if strings.Contains(msg, AgentBootloaderLog) {
							select {
							case p.output <- StepPrefix + InstallBootloaderStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step BootloaderStep")
							default:
							}
						} else if strings.Contains(msg, AgentRecoveryLog) {
							select {
							case p.output <- StepPrefix + InstallRecoveryStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step RecoveryStep")
							default:
							}
						} else if strings.Contains(msg, AgentPassiveLog) {
							select {
							case p.output <- StepPrefix + InstallPassiveStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step PassiveStep")
							default:
							}
						} else if strings.Contains(msg, AgentAfterInstallLog) && !strings.Contains(msg, "chroot") {
							select {
							case p.output <- StepPrefix + InstallAfterInstallStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step AfterInstallStep")
							default:
							}
						} else if strings.Contains(msg, AgentCompleteLog) {
							select {
							case p.output <- StepPrefix + InstallCompleteStep:
								internalLogger.Logger.Info().Msg("[Log Goroutine] Sent step CompleteStep")
							default:
							}
						}
					}
					lastLen = len(buf)
				}
				// Wait for installerDone before exiting
				select {
				case <-p.installerDone:
					if len(buf) == lastLen {
						internalLogger.Logger.Info().Msg("[Log Goroutine] Installer done and no new logs, exiting loop")
						break logLoop
					}
				default:
				}
			}
			internalLogger.Logger.Info().Msg("[Log Goroutine] Closing logsDone channel")
			close(p.logsDone) // Signal that log goroutine is done
		}()

		// Start installer goroutine (only one!)
		go func() {
			internalLogger.Logger.Info().Msg("[Installer Goroutine] Started")
			err := RunInstall(cc)
			internalLogger.Logger.Info().Msg("[Installer Goroutine] RunInstall returned")
			close(p.installerDone) // Signal installer is done
			<-p.logsDone           // Wait for logs to finish
			internalLogger.Logger.Info().Msg("[Installer Goroutine] Closing output and done channels")
			close(p.output) // Only close output here
			close(p.done)   // Only close done here
			_ = err         // ignore error, handled in logs
		}()
	})

	internalLogger.Logger.Info().Msg("[Init] Returning CheckInstallerMsg command")
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
		complete := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true).Render("Installation completed successfully!\nYou can now reboot your system.")
		s += "\n" + complete
	}

	return s
}

func (p *installProcessPage) Title() string {
	return "Installing"
}

func (p *installProcessPage) Help() string {
	if p.progress >= len(p.steps)-1 || p.errorMsg != "" {
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
	}
}
