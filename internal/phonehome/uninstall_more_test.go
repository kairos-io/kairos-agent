package phonehome_test

import (
	"errors"
	"os"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The cleanPhonehomeFromOEM inner loop has three failure modes we hadn't
// exercised: readFile surfacing a non-not-found error, writeFile failing
// while rewriting a partially-stripped file, and removeFile failing while
// deleting a file that only contained phonehome. Each of those must be
// carried up as the fatal error while the summary line names the path.

var _ = Describe("cleanPhonehomeFromOEM failure modes", func() {
	It("carries a read error up as fatal and names the path in the summary", func() {
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
		Expect(err).To(MatchError(readErr))
		Expect(summary).To(ContainSubstring("/oem/90_custom.yaml"), "summary missing offending path")
	})

	It("carries a write error up as fatal while rewriting a stripped file", func() {
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
		Expect(err).To(MatchError(writeErr))
		Expect(summary).To(ContainSubstring("writing /oem/90_custom.yaml"), "summary missing offending path")
	})

	It("carries a remove error up as fatal for a phonehome-only file", func() {
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
		Expect(err).To(MatchError(rmErr))
		Expect(summary).To(ContainSubstring("/oem/phonehome.yaml"), "summary missing offending path")
	})
})
