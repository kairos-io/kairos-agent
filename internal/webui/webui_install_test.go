package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"
	process "github.com/mudler/go-processmanager"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The spec below drives the branch in the /install handler that
// short-circuits to progress.html when a process is already tracked. We can't
// call p.Run() with a real /bin/sh because processmanager spins up a monitor
// goroutine that races with the /install handler's IsAlive() call — the
// library writes Process.PID from both without a lock (upstream
// process.go:69). Instead we build a stateless Process, drop an "exitcode"
// file in its state dir so ExitCode() reports "0", and rely on the handler's
// `IsAlive() || status == "0"` guard: IsAlive() is false (no pid file),
// status is "0", the redirect fires. No goroutine, no race.
var _ = Describe("install handler", func() {
	It("redirects to progress.html when a process is already running", func() {
		stateDir := GinkgoT().TempDir()
		p := process.New(process.WithStateDir(stateDir))
		Expect(os.WriteFile(filepath.Join(stateDir, "exitcode"), []byte("0"), 0600)).To(Succeed())

		s := &state{p: p}
		ec := buildRouterFromState(s)

		req := httptest.NewRequest(http.MethodPost, "/install", strings.NewReader("cloud-config=%23cloud-config%0A"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		ec.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusSeeOther), "expected redirect")
		Expect(rec.Header().Get("Location")).To(ContainSubstring("progress.html"))
	})
})

// buildRouterFromState is a test-only helper that mirrors buildRouter but
// takes the pre-built state. We can't call buildRouter directly because it
// takes an unexported type — but since we're in the internal package we can.
func buildRouterFromState(s *state) *echo.Echo {
	return buildRouter(s)
}
