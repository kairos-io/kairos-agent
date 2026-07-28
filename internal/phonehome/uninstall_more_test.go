package phonehome_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
)

// The cleanPhonehomeFromOEM inner loop has three failure modes we hadn't
// exercised: readFile surfacing a non-not-found error, writeFile failing
// while rewriting a partially-stripped file, and removeFile failing while
// deleting a file that only contained phonehome. Each of those must be
// carried up as the fatal error while the summary line names the path.

func TestCleanPhonehomeFromOEM_ReadError(t *testing.T) {
	readErr := errors.New("boom read")
	restore := phonehome.SetUninstallRunners(
		func(name string, args ...string) ([]byte, error) { return nil, nil },
		func(string) error { return os.ErrNotExist },
		func(path string) ([]byte, error) {
			if path == "/oem/90_custom.yaml" {
				return nil, readErr
			}
			return nil, os.ErrNotExist
		},
		func(string, []byte, os.FileMode) error { return nil },
		func(pattern string) ([]string, error) {
			if pattern == "/oem/*.yaml" {
				return []string{"/oem/90_custom.yaml"}, nil
			}
			return nil, nil
		},
	)
	defer restore()

	summary, err := phonehome.Uninstall(false)
	if !errors.Is(err, readErr) {
		t.Fatalf("expected read error, got %v", err)
	}
	if !strings.Contains(summary, "/oem/90_custom.yaml") {
		t.Errorf("summary missing offending path: %q", summary)
	}
}

func TestCleanPhonehomeFromOEM_WriteError(t *testing.T) {
	writeErr := errors.New("disk full")
	// The file has a phonehome key AND another key, so cleanPhonehomeFromOEM
	// takes the "rewrite via writeFile" branch, which we force to fail.
	restore := phonehome.SetUninstallRunners(
		func(name string, args ...string) ([]byte, error) { return nil, nil },
		func(string) error { return os.ErrNotExist },
		func(path string) ([]byte, error) {
			return []byte("#cloud-config\nphonehome:\n  url: x\nhostname: foo\n"), nil
		},
		func(string, []byte, os.FileMode) error { return writeErr },
		func(pattern string) ([]string, error) {
			if pattern == "/oem/*.yaml" {
				return []string{"/oem/90_custom.yaml"}, nil
			}
			return nil, nil
		},
	)
	defer restore()

	summary, err := phonehome.Uninstall(false)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
	if !strings.Contains(summary, "writing /oem/90_custom.yaml") {
		t.Errorf("summary missing offending path: %q", summary)
	}
}

func TestCleanPhonehomeFromOEM_RemoveEmptyFileError(t *testing.T) {
	// phonehome-only file, but removeFile fails with a non-ENOENT error →
	// caller surfaces it as fatal and the summary names the path.
	rmErr := errors.New("busy")
	restore := phonehome.SetUninstallRunners(
		func(name string, args ...string) ([]byte, error) { return nil, nil },
		func(path string) error {
			if path == "/oem/phonehome.yaml" {
				return rmErr
			}
			return os.ErrNotExist
		},
		func(path string) ([]byte, error) {
			if path == "/oem/phonehome.yaml" {
				return []byte("#cloud-config\nphonehome:\n  url: x\n"), nil
			}
			return nil, os.ErrNotExist
		},
		func(string, []byte, os.FileMode) error { return nil },
		func(pattern string) ([]string, error) {
			if pattern == "/oem/*.yaml" {
				return []string{"/oem/phonehome.yaml"}, nil
			}
			return nil, nil
		},
	)
	defer restore()

	summary, err := phonehome.Uninstall(false)
	if !errors.Is(err, rmErr) {
		t.Fatalf("expected remove error, got %v", err)
	}
	if !strings.Contains(summary, "/oem/phonehome.yaml") {
		t.Errorf("summary missing offending path: %q", summary)
	}
}
