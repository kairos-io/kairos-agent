package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	process "github.com/mudler/go-processmanager"
)

// TestInstall_RedirectsWhenProcessAlreadyRunning drives the branch in the
// /install handler that short-circuits to progress.html when a process is
// already tracked. We can't call p.Run() with a real /bin/sh because
// processmanager spins up a monitor goroutine that races with the /install
// handler's IsAlive() call — the library writes Process.PID from both without
// a lock (upstream process.go:69). Instead we build a stateless Process, drop
// an "exitcode" file in its state dir so ExitCode() reports "0", and rely on
// the handler's `IsAlive() || status == "0"` guard: IsAlive() is false (no pid
// file), status is "0", the redirect fires. No goroutine, no race.
func TestInstall_RedirectsWhenProcessAlreadyRunning(t *testing.T) {
	stateDir := t.TempDir()
	p := process.New(process.WithStateDir(stateDir))
	if err := os.WriteFile(filepath.Join(stateDir, "exitcode"), []byte("0"), 0600); err != nil {
		t.Fatal(err)
	}

	s := &state{p: p}
	ec := buildRouterFromState(s)

	req := httptest.NewRequest(http.MethodPost, "/install", strings.NewReader("cloud-config=%23cloud-config%0A"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ec.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "progress.html") {
		t.Errorf("unexpected Location: %q", loc)
	}
}

// buildRouterFromState is a test-only helper that mirrors buildRouter but
// takes the pre-built state. We can't call buildRouter directly because it
// takes an unexported type — but since we're in the internal package we can.
func buildRouterFromState(s *state) *echo.Echo {
	return buildRouter(s)
}
