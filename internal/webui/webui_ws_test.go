package webui_test

import (
	"net/http/httptest"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/webui"
	"golang.org/x/net/websocket"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("/ws stream endpoint", func() {
	It("sends a notice when no process is running", func() {
		e := webui.BuildRouter()
		ts := httptest.NewServer(e)
		defer ts.Close()

		// Convert http://host to ws://host
		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

		ws, err := websocket.Dial(wsURL, "", ts.URL)
		Expect(err).ToNot(HaveOccurred(), "dial")
		defer func() { _ = ws.Close() }()

		// Read one message; expect the "No process!" notice from the s.p==nil
		// branch.
		Expect(ws.SetReadDeadline(time.Now().Add(5 * time.Second))).To(Succeed())
		var msg string
		Expect(websocket.Message.Receive(ws, &msg)).To(Succeed(), "receive")
		Expect(msg).To(ContainSubstring("No process"), "unexpected message: %q", msg)
	})
})
