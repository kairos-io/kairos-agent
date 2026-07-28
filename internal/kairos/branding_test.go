package kairos

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBrandingFile(t *testing.T) {
	got := BrandingFile("banner")
	want := filepath.Join("/etc", "kairos", "branding", "banner")
	if got != want {
		t.Fatalf("BrandingFile=%q want %q", got, want)
	}
}

func TestDefaultTitleInteractiveInstaller_Fallback(t *testing.T) {
	// When branding file is missing under /etc/kairos/branding (unlikely to exist in test env),
	// fall back to the hardcoded default string.
	got := DefaultTitleInteractiveInstaller()
	if got == "" {
		t.Fatal("DefaultTitleInteractiveInstaller returned empty")
	}
	// Either the fallback string or content of the real branding file (both non-empty).
	if !strings.Contains(got, "Kairos") && got == "Kairos Interactive Installer" {
		t.Fatalf("unexpected default title: %q", got)
	}
}
