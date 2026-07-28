package webui_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/webui"
	"golang.org/x/net/websocket"
)

func TestStreamProcess_NoProcessSendsNotice(t *testing.T) {
	e := webui.BuildRouter()
	ts := httptest.NewServer(e)
	defer ts.Close()

	// Convert http://host to ws://host
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	ws, err := websocket.Dial(wsURL, "", ts.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Read one message; expect the "No process!" notice from the s.p==nil
	// branch.
	if err := ws.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var msg string
	if err := websocket.Message.Receive(ws, &msg); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if !strings.Contains(msg, "No process") {
		t.Fatalf("unexpected message: %q", msg)
	}
}
