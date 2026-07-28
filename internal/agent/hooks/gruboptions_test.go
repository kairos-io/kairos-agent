package hook

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
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

func TestExtractKcryptCmdline_NoValues(t *testing.T) {
	c := makeConfig()
	if got := extractKcryptCmdline(&c); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractKcryptCmdline_NoKcrypt(t *testing.T) {
	c := makeConfig()
	c.Collector.Values = collector.ConfigValues{"other": "x"}
	if got := extractKcryptCmdline(&c); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractKcryptCmdline_KcryptWrongType(t *testing.T) {
	c := makeConfig()
	c.Collector.Values = collector.ConfigValues{"kcrypt": "not-a-map"}
	if got := extractKcryptCmdline(&c); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractKcryptCmdline_NoChallenger(t *testing.T) {
	c := makeConfig()
	c.Collector.Values = collector.ConfigValues{
		"kcrypt": collector.ConfigValues{"other": "y"},
	}
	if got := extractKcryptCmdline(&c); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractKcryptCmdline_ChallengerWrongType(t *testing.T) {
	c := makeConfig()
	c.Collector.Values = collector.ConfigValues{
		"kcrypt": collector.ConfigValues{"challenger": "not-a-map"},
	}
	if got := extractKcryptCmdline(&c); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractKcryptCmdline_AllFields(t *testing.T) {
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
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in %q", s, got)
		}
	}
}

func TestExtractKcryptCmdline_MdnsFalseOmitted(t *testing.T) {
	c := makeConfig()
	c.Collector.Values = collector.ConfigValues{
		"kcrypt": collector.ConfigValues{
			"challenger": collector.ConfigValues{"mdns": false},
		},
	}
	if got := extractKcryptCmdline(&c); got != "" {
		t.Fatalf("mdns=false should not emit cmdline args, got %q", got)
	}
}
