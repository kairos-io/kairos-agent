package phonehome_test

import (
	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
