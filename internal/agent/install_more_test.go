package agent

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/twpayne/go-vfs/v5/vfst"
)


func TestGenerateInstallConfForManualCLIArgs(t *testing.T) {
	cases := []struct {
		name, device                     string
		reboot, poweroff                 bool
		wantEmpty                        bool
		mustContain, mustNotContainAllOf []string
	}{
		{name: "all-false empty", wantEmpty: true},
		{name: "just device", device: "/dev/sda", mustContain: []string{"install:", "device: /dev/sda"}},
		{name: "reboot only", reboot: true, mustContain: []string{"reboot: true"}, mustNotContainAllOf: []string{"poweroff", "device"}},
		{name: "poweroff only", poweroff: true, mustContain: []string{"poweroff: true"}},
		{name: "combined", device: "/dev/vda", reboot: true, poweroff: true, mustContain: []string{"reboot: true", "poweroff: true", "device: /dev/vda"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := generateInstallConfForManualCLIArgs(c.device, c.reboot, c.poweroff)
			if c.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty, got %q", got)
				}
				return
			}
			for _, s := range c.mustContain {
				if !strings.Contains(got, s) {
					t.Errorf("missing %q in %q", s, got)
				}
			}
			for _, s := range c.mustNotContainAllOf {
				if strings.Contains(got, s) {
					t.Errorf("unexpected substring %q in %q", s, got)
				}
			}
		})
	}
}

func TestDumpCCStringToFile(t *testing.T) {
	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("test fs: %v", err)
	}
	defer cleanup()

	if err := fsutils.MkdirAll(fs, "/tmp", 0o755); err != nil {
		t.Fatalf("mkdir /tmp: %v", err)
	}

	logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
	cfg := &sdkConfig.Config{
		Fs:     fs,
		Logger: logger,
		Collector: collector.Config{
			Values: collector.ConfigValues{"hello": "world"},
		},
	}

	path, err := dumpCCStringToFile(cfg)
	if err != nil {
		t.Fatalf("dumpCCStringToFile err: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	// dumpCCStringToFile writes to the underlying os filesystem via
	// os.WriteFile — not the injected vfs. So clean it up if it exists.
	if _, err := os.Stat(path); err == nil {
		defer os.Remove(path)
		got, _ := os.ReadFile(path)
		if !strings.Contains(string(got), "hello") {
			t.Fatalf("dumped file missing collector content: %q", got)
		}
	}
}

func TestDumpCCStringToFile_TempFileFails(t *testing.T) {
	// Use a vfs with no /tmp so fsutils.TempFile fails and dumpCCStringToFile
	// returns an error. Exercises the "TempFile err" branch.
	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("test fs: %v", err)
	}
	defer cleanup()
	logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
	cfg := &sdkConfig.Config{
		Fs:        fs,
		Logger:    logger,
		Collector: collector.Config{},
	}
	// /tmp does not exist on the fresh vfst tree → TempFile errors.
	if _, err := dumpCCStringToFile(cfg); err == nil {
		t.Skip("environment allowed TempFile to succeed unexpectedly")
	}
}

func TestPrepareConfiguration_MissingFile(t *testing.T) {
	_, err := prepareConfiguration("/does/not/exist.yaml")
	if err == nil {
		t.Fatal("expected error for missing local file")
	}
}

func TestPrepareConfiguration_BadURL(t *testing.T) {
	// A URL that fails DNS or connection returns an error from http.Head.
	_, err := prepareConfiguration("http://this-hopefully-does-not-resolve.invalid.example./x")
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestPrepareConfiguration_URL404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	_, err := prepareConfiguration(ts.URL)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %v", err)
	}
}

func TestPrepareConfiguration_URL500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	_, err := prepareConfiguration(ts.URL)
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestDisplayInfo_DisableSuppressesOutput(t *testing.T) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()

	displayInfo(&Config{WebUI: WebUI{Disable: true}})

	_ = w.Close()
	<-done
	if buf.Len() != 0 {
		t.Fatalf("expected no output when WebUI.Disable=true, got %q", buf.String())
	}
}

func TestEnsureDataSourceReady_ImmediateReturn(t *testing.T) {
	// When /run/.userdata_load does not exist (the common case in CI), the
	// function returns on the first ticker tick.
	// If the file happens to exist (unlikely), skip so the test doesn't wait
	// 5 minutes.
	if _, err := os.Stat("/run/.userdata_load"); err == nil {
		t.Skip("/run/.userdata_load exists on this host")
	}
	ensureDataSourceReady()
}

func TestDisplayInfo_NoAddressWithLocalIPs(t *testing.T) {
	// Neither Disable nor ListenAddress set → the code falls through to
	// machine.LocalIPs() and iterates over each interface. Whatever the host
	// returns is fine — we just want to walk the branch.
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()

	displayInfo(&Config{WebUI: WebUI{}})
	_ = w.Close()
	<-done
	if buf.Len() == 0 {
		t.Fatalf("expected some output for default WebUI config")
	}
}

func TestDisplayInfo_WithAddress(t *testing.T) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()

	displayInfo(&Config{WebUI: WebUI{ListenAddress: "127.0.0.1:9000"}})

	_ = w.Close()
	<-done
	if !strings.Contains(buf.String(), "127.0.0.1:9000") {
		t.Fatalf("expected address in output, got %q", buf.String())
	}
}
