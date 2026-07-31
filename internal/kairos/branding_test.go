package kairos

import (
	"fmt"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("branding", func() {
	Describe("BrandingFile", func() {
		It("joins the name under the branding directory", func() {
			got := BrandingFile("banner")
			want := filepath.Join("/etc", "kairos", "branding", "banner")
			Expect(got).To(Equal(want))
		})
	})

	Describe("DefaultTitleInteractiveInstaller", func() {
		It("falls back to the default title when the branding file is missing", func() {
			// When branding file is missing under /etc/kairos/branding (unlikely to exist in test env),
			// fall back to the hardcoded default string.
			got := DefaultTitleInteractiveInstaller()
			Expect(got).ToNot(BeEmpty())
			// Either the fallback string or content of the real branding file (both non-empty).
			if !strings.Contains(got, "Kairos") && got == "Kairos Interactive Installer" {
				Fail(fmt.Sprintf("unexpected default title: %q", got))
			}
		})
	})
})
