package phonehome_test

import (
	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseExtensionArgs", func() {
	It("parses a complete install request", func() {
		got, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type":      "sysext",
			"action":    "install",
			"name":      "tailscale-agent",
			"source":    "https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
			"bootState": "common",
			"now":       "true",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Type).To(Equal("sysext"))
		Expect(got.Action).To(Equal("install"))
		Expect(got.Name).To(Equal("tailscale-agent"))
		Expect(got.BootState).To(Equal("common"))
		Expect(got.Now).To(BeTrue())
	})

	It("rejects missing type", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{"action": "install"})
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("rejects an unsupported type", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "blob", "action": "install", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("requires source for action=install", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "install", "name": "x", "bootState": "common",
		})
		Expect(err).To(MatchError(ContainSubstring("source")))
	})

	It("requires bootState for action=enable", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "enable", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("requires bootState for action=disable", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "disable", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("requires name for every action", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "remove",
		})
		Expect(err).To(MatchError(ContainSubstring("name")))
	})

	It("rejects unsupported bootState", func() {
		_, err := phonehome.ParseExtensionArgsForTest(map[string]string{
			"type": "sysext", "action": "enable", "name": "x", "bootState": "wat",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})
})
