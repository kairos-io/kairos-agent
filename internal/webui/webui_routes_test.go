package webui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/webui"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func doForm(method, path, body string) *httptest.ResponseRecorder {
	GinkgoHelper()
	e := webui.BuildRouter()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func urlEncode(s string) string {
	// Minimal, hand-rolled percent-encoding to avoid dragging in net/url just
	// for readability in these tests.
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteByte('+')
		case r == '\n':
			b.WriteString("%0A")
		case r == ':':
			b.WriteString("%3A")
		case r == '/':
			b.WriteString("%2F")
		case r == '#':
			b.WriteString("%23")
		case r == '[':
			b.WriteString("%5B")
		case r == '-' || r == '_' || r == '.' || r == '~' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			b.WriteRune(r)
		default:
			// fall back to raw byte
			b.WriteRune(r)
		}
	}
	return b.String()
}

var _ = Describe("form routes", func() {
	Describe("/validate", func() {
		It("accepts a valid cloud-config", func() {
			body := "cloud-config=" + urlEncode("#cloud-config\nusers:\n  - name: kairos\n    passwd: kairos\n")
			rec := doForm(http.MethodPost, "/validate", body)
			Expect(rec.Code).To(Equal(http.StatusOK))
		})

		It("returns an error message for an invalid cloud-config", func() {
			body := "cloud-config=" + urlEncode("not a valid yaml at all: [")
			rec := doForm(http.MethodPost, "/validate", body)
			Expect(rec.Code).To(Equal(http.StatusOK))
			// invalid config → response body should be non-empty error message
			Expect(rec.Body.Len()).ToNot(BeZero(), "expected non-empty error body")
		})
	})

	Describe("/install", func() {
		It("responds when fields are missing", func() {
			// Missing installationDevice — the handler still spawns kairos-agent, but
			// /usr/bin/kairos-agent may not exist in test env → error path renders
			// message.html; either way the endpoint should respond 200 or 303.
			body := "cloud-config=" + urlEncode("#cloud-config\n")
			rec := doForm(http.MethodPost, "/install", body)
			Expect(rec.Code).To(Or(Equal(http.StatusOK), Equal(http.StatusSeeOther)), "unexpected status: %d", rec.Code)
		})

		It("responds when all options are set", func() {
			body := "cloud-config=" + urlEncode("#cloud-config\n") +
				"&reboot=on&power-off=on&installation-device=" + urlEncode("/dev/vda")
			rec := doForm(http.MethodPost, "/install", body)
			Expect(rec.Code).To(Or(Equal(http.StatusOK), Equal(http.StatusSeeOther)), "unexpected status: %d", rec.Code)
		})
	})
})
