package agent

import (
	"os"
	"os/exec"
)

const installerEnvVar = "KAIROS_INSTALLER"

// installerOverridePath and installerDefaultPath are the fixed image locations
// for the installer. The override is checked first so a customizer can drop
// their own binary next to (or in place of) the kairos-init-provided default.
// They are vars (not consts) so tests can point them at temp files.
var (
	installerOverridePath = "/system/installer/installer"
	installerDefaultPath  = "/system/installer/kairos-installer"
)

// resolveInstaller returns the path to an installer binary, or "" if none is
// found. Order: KAIROS_INSTALLER env (must exist) -> override path -> default.
func resolveInstaller() string {
	if p := os.Getenv(installerEnvVar); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range []string{installerOverridePath, installerDefaultPath} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// installerCommand builds the *exec.Cmd that invokes the installer, forwarding
// the install source when present. It does not wire stdio (see
// runExternalInstaller) so it can be unit-tested.
func installerCommand(path, source string) *exec.Cmd {
	args := []string{}
	if source != "" {
		args = append(args, "--source", source)
	}
	cmd := exec.Command(path, args...)
	cmd.Env = os.Environ()
	return cmd
}

// runExternalInstaller execs the installer with the current tty inherited and
// returns its error (an *exec.ExitError carries the installer's exit code).
func runExternalInstaller(path, source string) error {
	cmd := installerCommand(path, source)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
