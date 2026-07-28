package phonehome_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
)

// The tests in this file exercise the paths that hit /oem and
// exec.Command("kairos-agent",...) — production code paths that are
// otherwise untestable on a dev host. They rely on the SetOEMSeams and
// SetKairosAgentUpgrade helpers to redirect the writes and stub the exec.

// applyCloudConfigViaHandler drives the internal handleApplyCloudConfig
// through the public DefaultCommandHandler with an "allow apply-cloud-config"
// policy — that's the only door the internal package exposes for that path.
func applyCloudConfigViaHandler(cmd phonehome.CommandData) (string, error) {
	handler := phonehome.DefaultCommandHandler("http://example",
		func() string { return "" },
		func(c string) bool { return c == "apply-cloud-config" },
		nil, nil)
	return handler(cmd)
}

func TestWriteOEMCloudConfig_WritesFileWithHeader(t *testing.T) {
	dir := t.TempDir()
	restore := phonehome.SetOEMSeams(dir, func() error { return nil })
	defer restore()

	stopReboot := phonehome.SetRebootScheduler(func() {})
	defer stopReboot()

	// The handler prepends "#cloud-config" when it's missing.
	_, err := applyCloudConfigViaHandler(phonehome.CommandData{
		Command: "apply-cloud-config",
		Args:    map[string]string{"config": "users:\n  - name: foo\n"},
	})
	if err != nil {
		t.Fatalf("handleApplyCloudConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "99_phonehome_remote.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "#cloud-config\n") {
		t.Errorf("missing header, got %q", got)
	}
	if !strings.Contains(got, "name: foo") {
		t.Errorf("payload missing, got %q", got)
	}
}

func TestWriteOEMCloudConfig_PreservesExistingHeader(t *testing.T) {
	dir := t.TempDir()
	restore := phonehome.SetOEMSeams(dir, func() error { return nil })
	defer restore()

	original := "#cloud-config\nhostname: bar\n"
	_, err := applyCloudConfigViaHandler(phonehome.CommandData{
		Command: "apply-cloud-config",
		Args:    map[string]string{"config": original},
	})
	if err != nil {
		t.Fatalf("handleApplyCloudConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "99_phonehome_remote.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != original {
		t.Errorf("expected original preserved, got %q", string(data))
	}
	// Also ensures no double-prepended header.
	if strings.Count(string(data), "#cloud-config") != 1 {
		t.Errorf("header duplicated: %q", string(data))
	}
}

func TestWriteOEMCloudConfig_ReturnsWriteError(t *testing.T) {
	// Point the OEM dir at a path that cannot be created (a regular file where
	// a dir is expected). MkdirAll will fail (logged as a warning), and the
	// subsequent os.WriteFile call must surface the underlying error.
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "notadir")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	restore := phonehome.SetOEMSeams(blocker, func() error { return nil })
	defer restore()

	_, err := applyCloudConfigViaHandler(phonehome.CommandData{
		Command: "apply-cloud-config",
		Args:    map[string]string{"config": "hostname: x"},
	})
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestHandleUpgrade_KairosAgentSuccess(t *testing.T) {
	var gotArgs []string
	restoreExec := phonehome.SetKairosAgentUpgrade(func(_ context.Context, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("upgrade ok"), nil
	})
	defer restoreExec()

	rebooted := make(chan struct{}, 1)
	restoreReboot := phonehome.SetRebootScheduler(func() { rebooted <- struct{}{} })
	defer restoreReboot()

	cfg := &phonehome.Config{}
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

	// Direct source without any ":" → prefixed with "oci:"
	out, err := handler(phonehome.CommandData{
		Command: "upgrade",
		Args:    map[string]string{"source": "someref"},
	})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(out, "upgrade ok") {
		t.Errorf("output missing: %q", out)
	}
	if len(gotArgs) < 3 || gotArgs[0] != "upgrade" || gotArgs[1] != "--source" ||
		gotArgs[2] != "oci:someref" {
		t.Errorf("unexpected args: %v", gotArgs)
	}
	select {
	case <-rebooted:
	default:
		t.Error("expected reboot to be scheduled after upgrade")
	}
}

func TestHandleUpgrade_RecoveryDoesNotReboot(t *testing.T) {
	restoreExec := phonehome.SetKairosAgentUpgrade(func(_ context.Context, args ...string) ([]byte, error) {
		// verify --recovery is passed
		for _, a := range args {
			if a == "--recovery" {
				return []byte("done"), nil
			}
		}
		return nil, errors.New("missing --recovery")
	})
	defer restoreExec()

	rebooted := false
	restoreReboot := phonehome.SetRebootScheduler(func() { rebooted = true })
	defer restoreReboot()

	cfg := &phonehome.Config{}
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

	if _, err := handler(phonehome.CommandData{
		Command: "upgrade-recovery",
		Args:    map[string]string{"source": "oci:foo"},
	}); err != nil {
		t.Fatalf("upgrade-recovery: %v", err)
	}
	if rebooted {
		t.Error("upgrade-recovery must not schedule a reboot")
	}
}

func TestHandleUpgrade_KairosAgentFailure(t *testing.T) {
	restoreExec := phonehome.SetKairosAgentUpgrade(func(_ context.Context, args ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit 1")
	})
	defer restoreExec()

	rebooted := false
	restoreReboot := phonehome.SetRebootScheduler(func() { rebooted = true })
	defer restoreReboot()

	cfg := &phonehome.Config{}
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

	out, err := handler(phonehome.CommandData{
		Command: "upgrade",
		Args:    map[string]string{"source": "oci:foo"},
	})
	if err == nil {
		t.Fatal("expected exec error to surface")
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("output missing: %q", out)
	}
	if rebooted {
		t.Error("reboot must not fire on failure")
	}
}

func TestDefaultCommandHandler_Reboot(t *testing.T) {
	rebooted := false
	restore := phonehome.SetRebootScheduler(func() { rebooted = true })
	defer restore()

	cfg := &phonehome.Config{}
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)
	msg, err := handler(phonehome.CommandData{Command: "reboot"})
	if err != nil {
		t.Fatalf("reboot: %v", err)
	}
	if !strings.Contains(msg, "Rebooting") {
		t.Errorf("unexpected msg: %q", msg)
	}
	if !rebooted {
		t.Error("reboot not scheduled")
	}
}

func TestDefaultCommandHandler_ExecMissingArg(t *testing.T) {
	// exec is not in defaults; opt it in to hit the "missing command arg" branch.
	cfg := &phonehome.Config{AllowedCommands: []string{"exec"}}
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)
	_, err := handler(phonehome.CommandData{Command: "exec", Args: map[string]string{}})
	if err == nil {
		t.Fatal("expected error for missing exec command arg")
	}
}

func TestDefaultCommandHandler_ExecRunsShell(t *testing.T) {
	// A trivial `echo` will complete instantly and stays hermetic.
	cfg := &phonehome.Config{AllowedCommands: []string{"exec"}}
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)
	out, err := handler(phonehome.CommandData{Command: "exec", Args: map[string]string{"command": "echo kairos"}})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(out, "kairos") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestDefaultCommandHandler_UnknownCommand(t *testing.T) {
	// Fake allow-anything so the gate lets the unknown command through to the
	// default case in the switch.
	handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, func(string) bool { return true }, nil, nil)
	_, err := handler(phonehome.CommandData{Command: "wat"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got: %v", err)
	}
}

func TestHandleApplyCloudConfig_MissingConfigArg(t *testing.T) {
	handler := phonehome.DefaultCommandHandler("http://example",
		func() string { return "" },
		func(c string) bool { return c == "apply-cloud-config" },
		nil, nil)
	_, err := handler(phonehome.CommandData{Command: "apply-cloud-config", Args: map[string]string{}})
	if err == nil {
		t.Fatal("expected error")
	}
}
