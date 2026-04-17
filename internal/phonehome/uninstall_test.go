package phonehome

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestUninstall_HappyPath verifies Uninstall walks the full teardown sequence
// in the expected order and returns a no-error, step-by-step summary when
// every operation succeeds.
func TestUninstall_HappyPath(t *testing.T) {
	var systemctlCalls [][]string
	var removed []string

	origRun, origRm := runCommand, removeFile
	runCommand = func(name string, args ...string) ([]byte, error) {
		if name != "systemctl" {
			t.Fatalf("unexpected command %q", name)
		}
		systemctlCalls = append(systemctlCalls, args)
		return nil, nil
	}
	removeFile = func(path string) error {
		removed = append(removed, path)
		return nil
	}
	t.Cleanup(func() { runCommand, removeFile = origRun, origRm })

	summary, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall returned error: %v\nsummary:\n%s", err, summary)
	}

	// systemctl stop → disable → (service file removal) → daemon-reload.
	wantCmds := [][]string{
		{"stop", ServiceName},
		{"disable", ServiceName},
		{"daemon-reload"},
	}
	if len(systemctlCalls) != len(wantCmds) {
		t.Fatalf("systemctl calls: want %d, got %d (%v)", len(wantCmds), len(systemctlCalls), systemctlCalls)
	}
	for i := range wantCmds {
		if strings.Join(systemctlCalls[i], " ") != strings.Join(wantCmds[i], " ") {
			t.Errorf("systemctl call %d: want %v, got %v", i, wantCmds[i], systemctlCalls[i])
		}
	}

	// File removals in the documented order: unit file first, then creds, then
	// the two cloud-config files.
	wantRemoved := []string{
		ServicePath,
		DefaultCredentialsPath,
		CloudConfigPath,
		RemoteCloudConfigPath,
	}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed files: want %d, got %d (%v)", len(wantRemoved), len(removed), removed)
	}
	for i := range wantRemoved {
		if removed[i] != wantRemoved[i] {
			t.Errorf("removed[%d]: want %q, got %q", i, wantRemoved[i], removed[i])
		}
	}

	for _, tok := range []string{ServiceName, ServicePath, DefaultCredentialsPath, CloudConfigPath, RemoteCloudConfigPath} {
		if !strings.Contains(summary, tok) {
			t.Errorf("summary missing mention of %q; summary:\n%s", tok, summary)
		}
	}
}

// TestUninstall_IsIdempotent is the load-bearing guarantee. A node that is
// only partially installed (no service file, no credentials) must still tear
// down cleanly, and a second invocation of Uninstall against an already-clean
// node must also succeed.
func TestUninstall_IsIdempotent(t *testing.T) {
	origRun, origRm := runCommand, removeFile
	runCommand = func(name string, args ...string) ([]byte, error) {
		// systemctl exits non-zero for units it doesn't know about. Mimic that.
		if len(args) > 0 && (args[0] == "stop" || args[0] == "disable") {
			return []byte("Unit " + ServiceName + " not loaded."), errors.New("exit status 5")
		}
		return nil, nil
	}
	removeFile = func(path string) error { return os.ErrNotExist }
	t.Cleanup(func() { runCommand, removeFile = origRun, origRm })

	summary, err := Uninstall()
	if err != nil {
		t.Fatalf("expected nil error for idempotent teardown, got %v\nsummary:\n%s", err, summary)
	}
	for _, path := range []string{ServicePath, DefaultCredentialsPath, CloudConfigPath, RemoteCloudConfigPath} {
		if !strings.Contains(summary, "already absent: "+path) {
			t.Errorf("summary should note %q as already absent; got:\n%s", path, summary)
		}
	}
}

// TestUninstall_SurfacesRealFSErrors guards against silent swallowing of
// errors like permission-denied, which indicate a real problem the operator
// needs to know about (e.g. running the CLI without sudo).
func TestUninstall_SurfacesRealFSErrors(t *testing.T) {
	permDenied := &os.PathError{Op: "remove", Path: DefaultCredentialsPath, Err: os.ErrPermission}

	origRun, origRm := runCommand, removeFile
	runCommand = func(name string, args ...string) ([]byte, error) { return nil, nil }
	removeFile = func(path string) error {
		if path == DefaultCredentialsPath {
			return permDenied
		}
		return nil // other removals succeed
	}
	t.Cleanup(func() { runCommand, removeFile = origRun, origRm })

	summary, err := Uninstall()
	if err == nil {
		t.Fatalf("expected non-nil error when a real FS error occurs")
	}
	if !strings.Contains(summary, DefaultCredentialsPath) {
		t.Errorf("summary should name the path that failed; got:\n%s", summary)
	}
}
