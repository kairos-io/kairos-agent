package webui

import (
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	process "github.com/mudler/go-processmanager"
	"golang.org/x/net/websocket"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("streamProcess", func() {
	// The first spec drives the branch of streamProcess where s.p is non-nil
	// but IsAlive() returns false — i.e. the installer already exited. The
	// handler reads stderr/stdout, prints them, then emits "[INFO] Process
	// stopped!" and "[COMPLETE] Installation finished". No child process is
	// started; the pidless *Process reports IsAlive() == false because there's
	// no PID file to resolve.
	It("drains a dead process and sends the completion messages", func() {
		stateDir := GinkgoT().TempDir()
		// process.New with WithStateDir wires StdoutPath/StderrPath into our
		// temp directory. Without Run() no PID file is written, so IsAlive()
		// returns false — exactly the branch we want.
		p := process.New(process.WithStateDir(stateDir))
		Expect(os.WriteFile(p.StdoutPath(), []byte("hello-stdout\n"), 0600)).To(Succeed())
		Expect(os.WriteFile(p.StderrPath(), []byte("hello-stderr\n"), 0600)).To(Succeed())

		s := &state{p: p}

		ec := echo.New()
		ec.GET("/ws", streamProcess(s))
		ts := httptest.NewServer(ec)
		defer ts.Close()

		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
		ws, err := websocket.Dial(wsURL, "", ts.URL)
		Expect(err).ToNot(HaveOccurred(), "dial")
		defer ws.Close()

		// Drain until we see "[COMPLETE]" or hit the deadline. We accept any
		// interleaving because the handler streams line-by-line.
		_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		var full strings.Builder
		sawComplete := false
		for i := 0; i < 20 && !sawComplete; i++ {
			var msg string
			if err := websocket.Message.Receive(ws, &msg); err != nil {
				break
			}
			full.WriteString(msg)
			if strings.Contains(full.String(), "[COMPLETE]") {
				sawComplete = true
			}
		}
		body := full.String()
		Expect(body).To(ContainSubstring("hello-stdout"), "stdout content missing")
		Expect(body).To(ContainSubstring("hello-stderr"), "stderr content missing")
		Expect(body).To(ContainSubstring("Process stopped"), "stopped notice missing")
		Expect(sawComplete).To(BeTrue(), "[COMPLETE] not seen: %q", body)
	})

	// This spec targets the branch reached BEFORE we enter the tail-follow
	// loop: with s.p == nil we send a "No process!" notice and return. This
	// shadows the external-package test but runs on the internal state variant
	// so the same guard is exercised via streamProcess directly (giving the
	// coverage tool a distinct hit).
	It("sends a notice when there is no process", func() {
		ec := echo.New()
		ec.GET("/ws", streamProcess(&state{}))
		ts := httptest.NewServer(ec)
		defer ts.Close()

		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
		ws, err := websocket.Dial(wsURL, "", ts.URL)
		Expect(err).ToNot(HaveOccurred(), "dial")
		defer ws.Close()

		_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		var msg string
		Expect(websocket.Message.Receive(ws, &msg)).To(Succeed(), "receive")
		Expect(msg).To(ContainSubstring("No process"), "unexpected message: %q", msg)
	})
})

// The "live process" tail branch of streamProcess is not exercised as a unit
// test: spawning a real child through go-processmanager and then calling
// IsAlive() from the streamProcess handler triggers a data race inside
// processmanager itself (concurrent writes to Process.pid from the internal
// monitor goroutine and from readPID() on the handler side, upstream
// process.go:69). Because CI runs the suite under -race, that race turns into
// a Test Suite Failed with no failed spec. The dead-process and nil-process
// branches above give us the coverage; the live-process path is covered by
// the WebUI e2e installer flow.
