package webui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/internal/webui"
)

func doForm(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := webui.BuildRouter()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestValidate_Valid(t *testing.T) {
	body := "cloud-config=" + urlEncode("#cloud-config\nusers:\n  - name: kairos\n    passwd: kairos\n")
	rec := doForm(t, http.MethodPost, "/validate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestValidate_InvalidReturnsError(t *testing.T) {
	body := "cloud-config=" + urlEncode("not a valid yaml at all: [")
	rec := doForm(t, http.MethodPost, "/validate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	// invalid config → response body should be non-empty error message
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty error body")
	}
}

func TestInstall_MissingFields(t *testing.T) {
	// Missing installationDevice — the handler still spawns kairos-agent, but
	// /usr/bin/kairos-agent may not exist in test env → error path renders
	// message.html; either way the endpoint should respond 200 or 303.
	body := "cloud-config=" + urlEncode("#cloud-config\n")
	rec := doForm(t, http.MethodPost, "/install", body)
	if rec.Code != http.StatusOK && rec.Code != http.StatusSeeOther {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestInstall_AllOptions(t *testing.T) {
	body := "cloud-config=" + urlEncode("#cloud-config\n") +
		"&reboot=on&power-off=on&installation-device=" + urlEncode("/dev/vda")
	rec := doForm(t, http.MethodPost, "/install", body)
	if rec.Code != http.StatusOK && rec.Code != http.StatusSeeOther {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
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
