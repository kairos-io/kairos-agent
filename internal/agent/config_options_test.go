package agent_test

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/kairos-io/kairos-agent/v2/internal/agent"
)

var _ = Describe("agent config and options", func() {
	Describe("WebUI.HasAddress", func() {
		It("reports whether a listen address is set", func() {
			Expect((WebUI{}).HasAddress()).To(BeFalse(), "empty WebUI should have no address")
			Expect((WebUI{ListenAddress: ":8080"}).HasAddress()).To(BeTrue(), "non-empty ListenAddress should report HasAddress=true")
		})
	})

	Describe("LoadConfig", func() {
		It("returns defaults when the file is missing", func() {
			dir := GinkgoT().TempDir()
			// point at a file that does not exist; LoadConfig swallows read errors.
			c, err := LoadConfig(filepath.Join(dir, "missing.yaml"))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil(), "expected non-nil config even when file missing")
		})

		It("parses YAML", func() {
			dir := GinkgoT().TempDir()
			p := filepath.Join(dir, "agent.yaml")
			yaml := `fast: true
webui:
  disable: true
  listen_address: ":9999"
branding:
  interactive-install: HI
  install: INS
  reset: RST
  recovery: REC
`
			Expect(os.WriteFile(p, []byte(yaml), 0o600)).To(Succeed())
			c, err := LoadConfig(p)
			Expect(err).ToNot(HaveOccurred())
			Expect(c.Fast).To(BeTrue(), "expected fast=true")
			Expect(c.WebUI.Disable).To(BeTrue(), "expected webui.disable=true")
			Expect(c.WebUI.ListenAddress).To(Equal(":9999"))
			Expect(c.Branding.InteractiveInstall).To(Equal("HI"))
			Expect(c.Branding.Install).To(Equal("INS"))
			Expect(c.Branding.Reset).To(Equal("RST"))
			Expect(c.Branding.Recovery).To(Equal("REC"))
		})

		It("uses the default path when none is given", func() {
			// Default path is /etc/kairos/agent.yaml which likely doesn't exist in test
			// env — LoadConfig should still return a non-nil zero-value config.
			c, err := LoadConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil(), "expected non-nil config")
		})
	})

	Describe("Options.Apply", func() {
		It("applies all options", func() {
			o := &Options{}
			err := o.Apply(WithAPI("http://x"), WithDirectory("/a", "/b"), ForceAgent, RestartAgent)
			Expect(err).ToNot(HaveOccurred())
			Expect(o.APIAddress).To(Equal("http://x"))
			Expect(o.Dir).To(Equal([]string{"/a", "/b"}))
			Expect(o.Force).To(BeTrue(), "Force not set")
			Expect(o.Restart).To(BeTrue(), "Restart not set")
		})

		It("propagates errors from options", func() {
			boom := errors.New("boom")
			bad := func(_ *Options) error { return boom }
			o := &Options{}
			err := o.Apply(bad)
			Expect(errors.Is(err, boom)).To(BeTrue(), "expected boom, got %v", err)
		})
	})
})
