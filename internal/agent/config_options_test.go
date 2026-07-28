package agent_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	. "github.com/kairos-io/kairos-agent/v2/internal/agent"
)

func TestWebUI_HasAddress(t *testing.T) {
	if (WebUI{}).HasAddress() {
		t.Fatal("empty WebUI should have no address")
	}
	if !(WebUI{ListenAddress: ":8080"}).HasAddress() {
		t.Fatal("non-empty ListenAddress should report HasAddress=true")
	}
}

func TestLoadConfig_MissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	// point at a file that does not exist; LoadConfig swallows read errors
	c, err := LoadConfig(filepath.Join(dir, "missing.yaml"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil config even when file missing")
	}
}

func TestLoadConfig_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
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
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig err: %v", err)
	}
	if !c.Fast {
		t.Errorf("expected fast=true")
	}
	if !c.WebUI.Disable {
		t.Errorf("expected webui.disable=true")
	}
	if c.WebUI.ListenAddress != ":9999" {
		t.Errorf("listen_address=%q", c.WebUI.ListenAddress)
	}
	if c.Branding.InteractiveInstall != "HI" || c.Branding.Install != "INS" ||
		c.Branding.Reset != "RST" || c.Branding.Recovery != "REC" {
		t.Errorf("unexpected branding: %+v", c.Branding)
	}
}

func TestLoadConfig_NoPathsUsesDefault(t *testing.T) {
	// Default path is /etc/kairos/agent.yaml which likely doesn't exist in test
	// env — LoadConfig should still return a non-nil zero-value config.
	c, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig err: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestOptions_Apply(t *testing.T) {
	o := &Options{}
	err := o.Apply(WithAPI("http://x"), WithDirectory("/a", "/b"), ForceAgent, RestartAgent)
	if err != nil {
		t.Fatalf("Apply err: %v", err)
	}
	if o.APIAddress != "http://x" {
		t.Errorf("APIAddress=%q", o.APIAddress)
	}
	if len(o.Dir) != 2 || o.Dir[0] != "/a" || o.Dir[1] != "/b" {
		t.Errorf("Dir=%v", o.Dir)
	}
	if !o.Force || !o.Restart {
		t.Errorf("Force/Restart not set: %+v", o)
	}
}

func TestOptions_ApplyPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	bad := func(_ *Options) error { return boom }
	o := &Options{}
	err := o.Apply(bad)
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
}
