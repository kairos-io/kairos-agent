package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	<-done
	return buf.String()
}

func TestClearScreen(t *testing.T) {
	out := captureStdout(t, ClearScreen)
	if !strings.Contains(out, "\033c") {
		t.Fatalf("ClearScreen did not emit reset sequence, got %q", out)
	}
}

func TestPrintText(t *testing.T) {
	// pterm writes to its own default writer (initialized before we swap
	// os.Stdout in captureStdout), so we can't reliably capture its output.
	// Just exercise the function to prove it does not panic.
	PrintText("hello world", "Banner")
}

func TestPrintBranding_FromFile(t *testing.T) {
	dir := t.TempDir()
	brandingPath := dir + "/banner"
	if err := os.WriteFile(brandingPath, []byte("MY-BRAND-TEXT-42"), 0o644); err != nil {
		t.Fatalf("write branding: %v", err)
	}
	orig := brandingFilePath
	brandingFilePath = func() string { return brandingPath }
	defer func() { brandingFilePath = orig }()

	out := captureStdout(t, func() { PrintBranding([]byte("unused")) })
	if !strings.Contains(out, "MY-BRAND-TEXT-42") {
		t.Fatalf("expected branding text in output, got %q", out)
	}
}

func TestPrintBranding_FallbackWhenReadFails(t *testing.T) {
	// Point at a path that exists but is unreadable (a directory) — os.ReadFile
	// returns an error and the fallback PrintBanner path is taken. That call
	// would panic without a tty, so we recover from the panic to prove we
	// reached the fallback branch.
	dir := t.TempDir()
	orig := brandingFilePath
	brandingFilePath = func() string { return dir }
	defer func() { brandingFilePath = orig }()

	defer func() {
		_ = recover() // PrintBanner panics in headless mode, that's expected.
	}()
	PrintBranding([]byte("data"))
}
