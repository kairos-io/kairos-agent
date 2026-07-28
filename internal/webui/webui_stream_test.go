package webui

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	process "github.com/mudler/go-processmanager"
	"golang.org/x/net/websocket"
)

// TestStreamProcess_DeadProcessDrainsAndSendsCompletion drives the branch of
// streamProcess where s.p is non-nil but IsAlive() returns false — i.e. the
// installer already exited. The handler reads stderr/stdout, prints them,
// then emits "[INFO] Process stopped!" and "[COMPLETE] Installation
// finished". No child process is started; the pidless *Process reports
// IsAlive() == false because there's no PID file to resolve.
func TestStreamProcess_DeadProcessDrainsAndSendsCompletion(t *testing.T) {
	stateDir := t.TempDir()
	// process.New with WithStateDir wires StdoutPath/StderrPath into our
	// temp directory. Without Run() no PID file is written, so IsAlive()
	// returns false — exactly the branch we want.
	p := process.New(process.WithStateDir(stateDir))
	if err := os.WriteFile(p.StdoutPath(), []byte("hello-stdout\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.StderrPath(), []byte("hello-stderr\n"), 0600); err != nil {
		t.Fatal(err)
	}

	s := &state{p: p}

	ec := echo.New()
	ec.GET("/ws", streamProcess(s))
	ts := httptest.NewServer(ec)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
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
	if !strings.Contains(body, "hello-stdout") {
		t.Errorf("stdout content missing: %q", body)
	}
	if !strings.Contains(body, "hello-stderr") {
		t.Errorf("stderr content missing: %q", body)
	}
	if !strings.Contains(body, "Process stopped") {
		t.Errorf("stopped notice missing: %q", body)
	}
	if !sawComplete {
		t.Errorf("[COMPLETE] not seen: %q", body)
	}
}

// TestStreamProcess_NilProcess targets the branch reached BEFORE we enter the
// tail-follow loop: with s.p == nil we send a "No process!" notice and
// return. This shadows the external-package test but runs on the internal
// state variant so the same guard is exercised via streamProcess directly
// (giving the coverage tool a distinct hit).
func TestStreamProcess_NilProcess(t *testing.T) {
	ec := echo.New()
	ec.GET("/ws", streamProcess(&state{}))
	ts := httptest.NewServer(ec)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg string
	if err := websocket.Message.Receive(ws, &msg); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if !strings.Contains(msg, "No process") {
		t.Fatalf("unexpected message: %q", msg)
	}
}

// TestStreamProcess_LiveProcessThenExits drives the "tail-follow" branch:
// spawn a real /bin/sh that writes a line then exits, connect via WS, and
// verify we see both the tailed line and the [COMPLETE] marker. Skipped on
// systems without /bin/sh.
func TestStreamProcess_LiveProcessThenExits(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	stateDir := t.TempDir()
	p := process.New(
		process.WithName("/bin/sh"),
		process.WithArgs("-c", "echo tailed-line; sleep 0.3"),
		process.WithStateDir(stateDir),
	)
	if err := p.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}

	s := &state{p: p}
	ec := echo.New()
	ec.GET("/ws", streamProcess(s))
	ts := httptest.NewServer(ec)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ws.Close()

	_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
	var full strings.Builder
	for i := 0; i < 50; i++ {
		var msg string
		if err := websocket.Message.Receive(ws, &msg); err != nil {
			break
		}
		full.WriteString(msg)
		if strings.Contains(full.String(), "[COMPLETE]") {
			break
		}
	}
	body := full.String()
	if !strings.Contains(body, "tailed-line") {
		t.Errorf("tailed line missing: %q", body)
	}
	if !strings.Contains(body, "[COMPLETE]") {
		t.Errorf("[COMPLETE] missing: %q", body)
	}
}
