package phonehome_test

import (
	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newCollectorConfig builds an sdkConfig.Config whose Collector renders
// to the given phonehome-scoped values. This is exactly the shape produced
// by a real `config.Scan` on cloud-config files — the helper just skips
// the file I/O so LoadFromCollector can be tested in isolation.
func newCollectorConfig(phonehome map[string]interface{}) *sdkConfig.Config {
	values := collector.ConfigValues{}
	if phonehome != nil {
		values["phonehome"] = phonehome
	}
	return &sdkConfig.Config{
		Collector: collector.Config{Values: values},
	}
}

var _ = Describe("LoadFromCollector", func() {
	It("extracts a phonehome section from a merged cloud-config", func() {
		c := newCollectorConfig(map[string]interface{}{
			"url":                "https://example.invalid",
			"registration_token": "reg-123",
			"group":              "edge",
			"labels":             map[string]interface{}{"env": "prod"},
			"allowed_commands":   []interface{}{"exec", "reboot"},
		})

		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(cfg).ToNot(BeNil())
		Expect(cfg.URL).To(Equal("https://example.invalid"))
		Expect(cfg.RegistrationToken).To(Equal("reg-123"))
		Expect(cfg.Group).To(Equal("edge"))
		Expect(cfg.Labels).To(HaveKeyWithValue("env", "prod"))
		Expect(cfg.AllowedCommands).To(ConsistOf("exec", "reboot"))
	})

	It("returns ok=false when the phonehome section is absent", func() {
		c := newCollectorConfig(nil)
		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(cfg).To(BeNil())
	})

	It("returns ok=false when phonehome.url is empty", func() {
		c := newCollectorConfig(map[string]interface{}{
			"group": "edge",
		})
		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(cfg).To(BeNil())
	})

	It("returns ok=false on a nil sdkConfig.Config pointer", func() {
		cfg, ok, err := phonehome.LoadFromCollector(nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(cfg).To(BeNil())
	})

	It("round-trips the full Config back through IsAllowed", func() {
		c := newCollectorConfig(map[string]interface{}{
			"url":              "https://example.invalid",
			"allowed_commands": []interface{}{"exec"},
		})
		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		// The policy parsed out of the Collector must drive IsAllowed correctly.
		Expect(cfg.IsAllowed("exec")).To(BeTrue())
		Expect(cfg.IsAllowed("reboot")).To(BeFalse())
	})
})

var _ = Describe("Config.IsAllowed", func() {
	It("allows the default safe set when AllowedCommands is nil", func() {
		cfg := &phonehome.Config{}
		Expect(cfg.IsAllowed("upgrade")).To(BeTrue())
		Expect(cfg.IsAllowed("upgrade-recovery")).To(BeTrue())
		Expect(cfg.IsAllowed("reboot")).To(BeTrue())
	})

	It("denies destructive commands by default", func() {
		cfg := &phonehome.Config{}
		Expect(cfg.IsAllowed("exec")).To(BeFalse())
		Expect(cfg.IsAllowed("reset")).To(BeFalse())
		Expect(cfg.IsAllowed("apply-cloud-config")).To(BeFalse())
	})

	It("honors an explicit AllowedCommands allowlist", func() {
		cfg := &phonehome.Config{AllowedCommands: []string{"exec"}}
		Expect(cfg.IsAllowed("exec")).To(BeTrue())
		// Explicit list replaces defaults — reboot is no longer allowed.
		Expect(cfg.IsAllowed("reboot")).To(BeFalse())
	})

	It("treats an empty (non-nil) slice as deny-all", func() {
		cfg := &phonehome.Config{AllowedCommands: []string{}}
		Expect(cfg.IsAllowed("upgrade")).To(BeFalse())
		Expect(cfg.IsAllowed("reboot")).To(BeFalse())
	})
})

var _ = Describe("DefaultCommandHandler policy gating", func() {
	It("rejects commands that are not in the allowlist", func() {
		cfg := &phonehome.Config{} // defaults — exec not allowed
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed)

		out, err := handler(phonehome.CommandData{
			ID:      "c1",
			Command: "exec",
			Args:    map[string]string{"command": "echo hi"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not permitted"))
		Expect(out).To(BeEmpty())
	})

	It("rejects unknown commands even when listed in defaults", func() {
		cfg := &phonehome.Config{}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed)

		_, err := handler(phonehome.CommandData{ID: "c2", Command: "totally-made-up"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not permitted"))
	})

	It("denies everything when isAllowed is nil (fail-closed)", func() {
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, nil)

		_, err := handler(phonehome.CommandData{ID: "c3", Command: "upgrade"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not permitted"))
	})
})
