package phonehome

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runCommand is the package-local indirection used so tests can substitute a
// fake runner for the systemctl invocations Uninstall issues. Production code
// goes through exec.Command and captures combined stdout+stderr for the
// human-readable summary.
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput() //nosec G204 -- callers pass fixed systemctl subcommands only
}

// removeFile exists as a variable for the same reason as runCommand: the
// Uninstall flow is effectful against real paths in production and we want
// tests to swap it out without having to build a full fake filesystem.
var removeFile = func(path string) error { return os.Remove(path) }

// Uninstall runs the on-host phone-home teardown: stop+disable the systemd
// service, remove the unit file, reload systemd, drop the phonehome
// cloud-config files, and wipe the saved credentials.
//
// Every step is best-effort — missing files or already-stopped units are
// reported in the summary but do not fail the call. This is deliberate:
// running Uninstall twice in a row must not error, and a half-installed
// node (where e.g. the service unit was never written) must still converge
// to the fully-cleaned-up state. The returned error is non-nil only if
// something unexpected happened that the caller should surface (permission
// denied, I/O errors that aren't "not found").
//
// The returned string is the operator-facing summary: each line is a step
// result like "stopped kairos-agent-phonehome" or "removed /oem/phonehome.yaml".
// It is what the remote `unregister` command returns as its result over the
// phonehome WebSocket, and what the local CLI prints to stdout.
func Uninstall() (string, error) {
	var lines []string
	var fatal error

	// systemctl steps — swallow failures from units that don't exist.
	for _, step := range []struct {
		label string
		args  []string
	}{
		{"stopping " + ServiceName, []string{"stop", ServiceName}},
		{"disabling " + ServiceName, []string{"disable", ServiceName}},
	} {
		out, err := runCommand("systemctl", step.args...)
		if err != nil {
			// systemctl returns non-zero for "not loaded" / "not enabled".
			// Note it in the log but don't fail the overall teardown.
			lines = append(lines, fmt.Sprintf("%s: %s (%s)", step.label, strings.TrimSpace(string(out)), err))
			continue
		}
		lines = append(lines, step.label+": ok")
	}

	// Unit file.
	if err := removeFile(ServicePath); err != nil {
		if os.IsNotExist(err) {
			lines = append(lines, "unit file already absent: "+ServicePath)
		} else {
			lines = append(lines, fmt.Sprintf("removing %s: %v", ServicePath, err))
			if fatal == nil {
				fatal = err
			}
		}
	} else {
		lines = append(lines, "removed "+ServicePath)
	}

	// daemon-reload so the removed unit drops from the unit cache.
	if out, err := runCommand("systemctl", "daemon-reload"); err != nil {
		lines = append(lines, fmt.Sprintf("daemon-reload: %s (%s)", strings.TrimSpace(string(out)), err))
	} else {
		lines = append(lines, "daemon-reload: ok")
	}

	// File cleanup. All three paths are owned by the phonehome install, so
	// missing-file is just "already cleaned up" rather than an error.
	for _, path := range []string{
		DefaultCredentialsPath,
		CloudConfigPath,
		RemoteCloudConfigPath,
	} {
		if err := removeFile(path); err != nil {
			if os.IsNotExist(err) {
				lines = append(lines, "already absent: "+path)
				continue
			}
			lines = append(lines, fmt.Sprintf("removing %s: %v", path, err))
			if fatal == nil {
				fatal = err
			}
			continue
		}
		lines = append(lines, "removed "+path)
	}

	return strings.Join(lines, "\n"), fatal
}
