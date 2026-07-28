package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	process "github.com/mudler/go-processmanager"
)

// TestInstall_RedirectsWhenProcessAlreadyRunning drives the branch in the
// /install handler that short-circuits to progress.html when a process is
// already tracked. We use an internal test so we can hand-build the router
// with a pre-populated state — the exported BuildRouter always starts with
// s.p == nil.
func TestInstall_RedirectsWhenProcessAlreadyRunning(t *testing.T) {
	stateDir := t.TempDir()
	p := process.New(
		process.WithName("/bin/sh"),
		process.WithArgs("-c", "sleep 2"),
		process.WithStateDir(stateDir),
	)
	if err := p.Run(); err != nil {
		t.Skipf("cannot spawn /bin/sh: %v", err)
	}
	defer func() { _ = p.Stop() }()

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
