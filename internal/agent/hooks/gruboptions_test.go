package hook

import (
	"bytes"

	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
)

func makeConfig() sdkConfig.Config {
	buf := &bytes.Buffer{}
	logger := sdkLogger.NewBufferLogger(buf)
	logger.SetLevel("debug")
	// vfs.OSFS satisfies the sdkFs.KairosFS interface, so helpers that use
	// c.Fs (SetPersistentVariables etc.) do not nil-panic. We tolerate their
	// errors — the point of these tests is to exercise the code paths, not to
	// perform real grubenv writes.
	return sdkConfig.Config{Logger: logger, Collector: collector.Config{}, Fs: vfs.OSFS}
}

var _ = Describe("extractKcryptCmdline", func() {
	It("returns empty with no values", func() {
		c := makeConfig()
		Expect(extractKcryptCmdline(&c)).To(BeEmpty())
	})

	It("returns empty without a kcrypt key", func() {
		c := makeConfig()
		c.Collector.Values = collector.ConfigValues{"other": "x"}
		Expect(extractKcryptCmdline(&c)).To(BeEmpty())
	})

	It("returns empty when kcrypt has the wrong type", func() {
		c := makeConfig()
		c.Collector.Values = collector.ConfigValues{"kcrypt": "not-a-map"}
		Expect(extractKcryptCmdline(&c)).To(BeEmpty())
	})

	It("returns empty without a challenger key", func() {
		c := makeConfig()
		c.Collector.Values = collector.ConfigValues{
			"kcrypt": collector.ConfigValues{"other": "y"},
		}
		Expect(extractKcryptCmdline(&c)).To(BeEmpty())
	})

	It("returns empty when challenger has the wrong type", func() {
		c := makeConfig()
		c.Collector.Values = collector.ConfigValues{
			"kcrypt": collector.ConfigValues{"challenger": "not-a-map"},
		}
		Expect(extractKcryptCmdline(&c)).To(BeEmpty())
	})

	It("emits all challenger fields", func() {
		c := makeConfig()
		c.Collector.Values = collector.ConfigValues{
			"kcrypt": collector.ConfigValues{
				"challenger": collector.ConfigValues{
					"challenger_server": "http://server:1234",
					"mdns":              true,
					"certificate":       "CERT",
					"nv_index":          "0x81000000",
					"c_index":           "0x81000001",
					"tpm_device":        "/dev/tpm0",
				},
			},
		}
		got := extractKcryptCmdline(&c)
		for _, s := range []string{
			"kcrypt.challenger.challenger_server=http://server:1234",
			"kcrypt.challenger.mdns=true",
			"kcrypt.challenger.certificate=CERT",
			"kcrypt.challenger.nv_index=0x81000000",
			"kcrypt.challenger.c_index=0x81000001",
			"kcrypt.challenger.tpm_device=/dev/tpm0",
		} {
			Expect(got).To(ContainSubstring(s))
		}
	})

	It("omits mdns when false", func() {
		c := makeConfig()
		c.Collector.Values = collector.ConfigValues{
			"kcrypt": collector.ConfigValues{
				"challenger": collector.ConfigValues{"mdns": false},
			},
		}
		Expect(extractKcryptCmdline(&c)).To(BeEmpty(), "mdns=false should not emit cmdline args")
	})
})
