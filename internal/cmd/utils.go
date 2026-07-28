package cmd

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos-agent/v2/internal/kairos"

	"github.com/kairos-io/kairos-sdk/utils"
	"github.com/pterm/pterm"
)

func PrintText(f string, banner string) {
	pterm.DefaultBox.WithTitle(banner).WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		f)
}

func ClearScreen() {
	fmt.Print("\033c")
}

// brandingFilePath is a variable so tests can override the on-disk lookup
// location (the production path is not writable in unit tests, and the fallback
// PrintBanner call requires a real terminal).
var brandingFilePath = func() string { return kairos.BrandingFile("banner") }

// SetBrandingFileForTests overrides the branding file lookup path. Returning
// the previous value lets tests restore it in a defer. Intended for tests
// only; production code must not call this.
func SetBrandingFileForTests(path string) (restore func()) {
	prev := brandingFilePath
	brandingFilePath = func() string { return path }
	return func() { brandingFilePath = prev }
}

func PrintBranding(b []byte) {
	brandingFile := brandingFilePath()
	if _, err := os.Stat(brandingFile); err == nil {
		f, err := os.ReadFile(brandingFile)
		if err == nil {
			fmt.Println(string(f))
			return
		}
	}
	utils.PrintBanner(b)
}
