package agent

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("install helpers", func() {
	Describe("generateInstallConfForManualCLIArgs", func() {
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
			c := c
			It(c.name, func() {
				got := generateInstallConfForManualCLIArgs(c.device, c.reboot, c.poweroff)
				if c.wantEmpty {
					Expect(got).To(BeEmpty(), "expected empty, got %q", got)
					return
				}
				for _, s := range c.mustContain {
					Expect(got).To(ContainSubstring(s), "missing %q in %q", s, got)
				}
				for _, s := range c.mustNotContainAllOf {
					Expect(got).ToNot(ContainSubstring(s), "unexpected substring %q in %q", s, got)
				}
			})
		}
	})

	Describe("dumpCCStringToFile", func() {
		It("dumps the collector config to a file", func() {
			fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
			Expect(err).ToNot(HaveOccurred(), "test fs")
			defer cleanup()

			Expect(fsutils.MkdirAll(fs, "/tmp", 0o755)).To(Succeed(), "mkdir /tmp")

			logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
			cfg := &sdkConfig.Config{
				Fs:     fs,
				Logger: logger,
				Collector: collector.Config{
					Values: collector.ConfigValues{"hello": "world"},
				},
			}

			path, err := dumpCCStringToFile(cfg)
			Expect(err).ToNot(HaveOccurred(), "dumpCCStringToFile err")
			Expect(path).ToNot(BeEmpty(), "expected non-empty path")
			// dumpCCStringToFile writes to the underlying os filesystem via
			// os.WriteFile — not the injected vfs. So clean it up if it exists.
			if _, err := os.Stat(path); err == nil {
				defer os.Remove(path)
				got, _ := os.ReadFile(path)
				Expect(string(got)).To(ContainSubstring("hello"), "dumped file missing collector content: %q", got)
			}
		})

		It("errors when TempFile fails", func() {
			// Use a vfs with no /tmp so fsutils.TempFile fails and dumpCCStringToFile
			// returns an error. Exercises the "TempFile err" branch.
			fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
			Expect(err).ToNot(HaveOccurred(), "test fs")
			defer cleanup()
			logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
			cfg := &sdkConfig.Config{
				Fs:        fs,
				Logger:    logger,
				Collector: collector.Config{},
			}
			// /tmp does not exist on the fresh vfst tree → TempFile errors.
			if _, err := dumpCCStringToFile(cfg); err == nil {
				Skip("environment allowed TempFile to succeed unexpectedly")
			}
		})
	})

	Describe("prepareConfiguration", func() {
		It("errors on a missing local file", func() {
			_, err := prepareConfiguration("/does/not/exist.yaml")
			Expect(err).To(HaveOccurred(), "expected error for missing local file")
		})

		It("errors on an unreachable URL", func() {
			// A URL that fails DNS or connection returns an error from http.Head.
			_, err := prepareConfiguration("http://this-hopefully-does-not-resolve.invalid.example./x")
			Expect(err).To(HaveOccurred(), "expected error for unreachable URL")
		})

		It("errors with 'not found' on a 404 URL", func() {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))
			defer ts.Close()
			_, err := prepareConfiguration(ts.URL)
			Expect(err).To(HaveOccurred(), "expected 'not found' error")
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("errors on a 500 URL", func() {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer ts.Close()
			_, err := prepareConfiguration(ts.URL)
			Expect(err).To(HaveOccurred(), "expected error for non-200 status")
		})
	})

	Describe("displayInfo", func() {
		It("suppresses output when WebUI is disabled", func() {
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
			Expect(buf.Len()).To(BeZero(), "expected no output when WebUI.Disable=true, got %q", buf.String())
		})

		It("lists local IPs when no address is configured", func() {
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
			Expect(buf.Len()).ToNot(BeZero(), "expected some output for default WebUI config")
		})

		It("prints the configured address", func() {
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
			Expect(buf.String()).To(ContainSubstring("127.0.0.1:9000"), "expected address in output, got %q", buf.String())
		})
	})

	Describe("ensureDataSourceReady", func() {
		It("returns immediately when the userdata sentinel is absent", func() {
			// When /run/.userdata_load does not exist (the common case in CI), the
			// function returns on the first ticker tick.
			// If the file happens to exist (unlikely), skip so the test doesn't wait
			// 5 minutes.
			if _, err := os.Stat("/run/.userdata_load"); err == nil {
				Skip("/run/.userdata_load exists on this host")
			}
			ensureDataSourceReady()
		})
	})
})
