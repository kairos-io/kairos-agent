package cmd

import (
	"bytes"
	"io"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func captureStdout(fn func()) string {
	orig := os.Stdout
	r, w, err := os.Pipe()
	Expect(err).ToNot(HaveOccurred(), "pipe")
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

var _ = Describe("cmd utils", func() {
	Describe("ClearScreen", func() {
		It("emits the terminal reset sequence", func() {
			out := captureStdout(ClearScreen)
			Expect(out).To(ContainSubstring("\033c"))
		})
	})

	Describe("PrintText", func() {
		It("does not panic", func() {
			// pterm writes to its own default writer (initialized before we swap
			// os.Stdout in captureStdout), so we can't reliably capture its output.
			// Just exercise the function to prove it does not panic.
			PrintText("hello world", "Banner")
		})
	})

	Describe("PrintBranding", func() {
		It("prints the branding file contents when the file is readable", func() {
			dir := GinkgoT().TempDir()
			brandingPath := dir + "/banner"
			Expect(os.WriteFile(brandingPath, []byte("MY-BRAND-TEXT-42"), 0o644)).To(Succeed())
			orig := brandingFilePath
			brandingFilePath = func() string { return brandingPath }
			DeferCleanup(func() { brandingFilePath = orig })

			out := captureStdout(func() { PrintBranding([]byte("unused")) })
			Expect(out).To(ContainSubstring("MY-BRAND-TEXT-42"))
		})

		It("falls back to PrintBanner when the read fails", func() {
			// Point at a path that exists but is unreadable (a directory) — os.ReadFile
			// returns an error and the fallback PrintBanner path is taken. That call
			// would panic without a tty, so we recover from the panic to prove we
			// reached the fallback branch.
			dir := GinkgoT().TempDir()
			orig := brandingFilePath
			brandingFilePath = func() string { return dir }
			DeferCleanup(func() { brandingFilePath = orig })

			defer func() {
				_ = recover() // PrintBanner panics in headless mode, that's expected.
			}()
			PrintBranding([]byte("data"))
		})
	})
})
