package phonehome_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// commandRecorder captures shell-outs and always returns a command that
// exits successfully. Tests that need failure use a failingRecorder
// instead (added in Task 5).
type commandRecorder struct {
	calls [][]string
}

func (r *commandRecorder) record(name string, args ...string) *exec.Cmd {
	r.calls = append(r.calls, append([]string{name}, args...))
	return exec.Command("/bin/true")
}

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

var _ = Describe("DefaultCommandHandler — extension command", func() {
	It("rejects the command when isAllowed returns false", func() {
		denyAll := func(string) bool { return false }
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, denyAll, nil)
		_, err := handler(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "remove", "name": "x"},
		})
		Expect(err).To(MatchError(ContainSubstring("not permitted")))
	})

	It("surfaces parse errors when args are malformed", func() {
		allow := func(string) bool { return true }
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, allow, nil)
		_, err := handler(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{"type": "wat"},
		})
		Expect(err).To(MatchError(ContainSubstring("unsupported type")))
	})

	It("dispatches to handleExtension when args validate", func() {
		rec := &commandRecorder{}
		restore := phonehome.SetExecCommand(rec.record)
		defer restore()
		allow := func(string) bool { return true }
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, allow, nil)
		_, err := handler(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "remove", "name": "tailscale-agent",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "remove", "tailscale-agent"},
		}))
	})
})

var _ = Describe("handleExtension — install action", func() {
	var rec *commandRecorder
	var restore func()

	BeforeEach(func() {
		rec = &commandRecorder{}
		restore = phonehome.SetExecCommand(rec.record)
	})
	AfterEach(func() { restore() })

	It("issues install + enable with --now when now=true", func() {
		out, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{
				"type":      "sysext",
				"action":    "install",
				"name":      "tailscale-agent",
				"source":    "https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
				"bootState": "common",
				"now":       "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("installed"))
		Expect(rec.calls).To(HaveLen(2))
		Expect(rec.calls[0]).To(Equal([]string{
			"kairos-agent", "sysext", "install",
			"https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
		}))
		Expect(rec.calls[1]).To(Equal([]string{
			"kairos-agent", "sysext", "enable", "--common", "--now", "tailscale-agent",
		}))
	})

	It("omits --now when now=false", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "confext", "action": "install",
				"name":      "fluent-bit-config",
				"source":    "https://x/file?token=k",
				"bootState": "active",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(2))
		Expect(rec.calls[1]).To(Equal([]string{
			"kairos-agent", "confext", "enable", "--active", "fluent-bit-config",
		}))
	})
})

var _ = Describe("handleExtension — enable/disable/remove", func() {
	var rec *commandRecorder
	var restore func()

	BeforeEach(func() {
		rec = &commandRecorder{}
		restore = phonehome.SetExecCommand(rec.record)
	})
	AfterEach(func() { restore() })

	It("issues enable without --now when now=false", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "enable",
				"name": "tailscale-agent", "bootState": "passive", "now": "false",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "enable", "--passive", "tailscale-agent"},
		}))
	})

	It("issues disable with --now when now=true", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "confext", "action": "disable",
				"name": "fluent-bit-config", "bootState": "common", "now": "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "confext", "disable", "--common", "--now", "fluent-bit-config"},
		}))
	})

	It("issues remove with --now when now=true", func() {
		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "remove",
				"name": "tailscale-agent", "now": "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "remove", "--now", "tailscale-agent"},
		}))
	})
})

// failingRecorder is like commandRecorder but lets the test request a
// specific call (0-based) to exit non-zero.
type failingRecorder struct {
	calls  [][]string
	failOn int
}

func (r *failingRecorder) record(name string, args ...string) *exec.Cmd {
	idx := len(r.calls)
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.failOn == idx {
		return exec.Command("/bin/false")
	}
	return exec.Command("/bin/true")
}

var _ = Describe("handleExtension — install error paths", func() {
	It("returns the install error and does NOT call enable when download fails", func() {
		rec := &failingRecorder{failOn: 0}
		restore := phonehome.SetExecCommand(rec.record)
		defer restore()

		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "install",
				"name": "x", "source": "https://x/y", "bootState": "common",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extension install")))
		Expect(rec.calls).To(HaveLen(1))
	})

	It("returns the enable error when symlink creation fails after a successful download", func() {
		rec := &failingRecorder{failOn: 1}
		restore := phonehome.SetExecCommand(rec.record)
		defer restore()

		_, err := phonehome.HandleExtensionForTest(phonehome.CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "install",
				"name": "x", "source": "https://x/y", "bootState": "common",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extension enable")))
		Expect(rec.calls).To(HaveLen(2))
	})
})

var _ = Describe("parseBundledExtensions", func() {
	It("returns an empty slice for an empty input", func() {
		got, err := phonehome.ParseBundledExtensionsForTest("")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("decodes a well-formed array", func() {
		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "tailscale-agent", Source: "https://x/a"},
			{Type: "confext", Name: "fluent-bit-config", Source: "https://x/b"},
		})
		got, err := phonehome.ParseBundledExtensionsForTest(string(raw))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].Name).To(Equal("tailscale-agent"))
		Expect(got[1].Type).To(Equal("confext"))
	})

	It("rejects an unsupported type", func() {
		_, err := phonehome.ParseBundledExtensionsForTest(
			`[{"type":"blob","name":"x","source":"https://x"}]`)
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("rejects a missing name", func() {
		_, err := phonehome.ParseBundledExtensionsForTest(
			`[{"type":"sysext","source":"https://x"}]`)
		Expect(err).To(MatchError(ContainSubstring("name")))
	})

	It("rejects a missing source", func() {
		_, err := phonehome.ParseBundledExtensionsForTest(
			`[{"type":"sysext","name":"x"}]`)
		Expect(err).To(MatchError(ContainSubstring("source")))
	})
})

var _ = Describe("extensionEnabledAnywhere", func() {
	var rootRestore func()
	var tmpRoot string

	BeforeEach(func() {
		tmpRoot = GinkgoT().TempDir()
		rootRestore = phonehome.SetExtensionsPersistentRoot(func(extType string) string {
			return filepath.Join(tmpRoot, extType+"s")
		})
	})
	AfterEach(func() { rootRestore() })

	It("returns false when no scope dir contains the extension", func() {
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeFalse())
	})

	It("returns true when a symlink exists under active/", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "active")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeTrue())
	})

	It("returns true when a symlink exists under common/", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeTrue())
	})

	It("matches by prefix-then-dot so a longer-named neighbour doesn't false-positive", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent-helper.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(phonehome.ExtensionEnabledAnywhereForTest("sysext", "tailscale-agent")).To(BeFalse())
	})
})

var _ = Describe("installBundledExtension", func() {
	var rec *commandRecorder
	var restoreExec, restoreRoot func()
	var tmpRoot string

	BeforeEach(func() {
		tmpRoot = GinkgoT().TempDir()
		restoreRoot = phonehome.SetExtensionsPersistentRoot(func(extType string) string {
			return filepath.Join(tmpRoot, extType+"s")
		})
		rec = &commandRecorder{}
		restoreExec = phonehome.SetExecCommand(rec.record)
	})
	AfterEach(func() {
		restoreExec()
		restoreRoot()
	})

	It("installs and enables a brand-new extension at --active", func() {
		Expect(phonehome.InstallBundledExtensionForTest(
			phonehome.BundledExtension{Type: "sysext", Name: "tailscale-agent", Source: "https://x/y"},
			"active",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/y"},
			{"kairos-agent", "sysext", "enable", "--active", "tailscale-agent"},
		}))
	})

	It("skips enable when the extension is already present at common scope", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())

		Expect(phonehome.InstallBundledExtensionForTest(
			phonehome.BundledExtension{Type: "sysext", Name: "tailscale-agent", Source: "https://x/y"},
			"active",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/y"},
		}))
	})

	It("enables at the recovery scope when called with scope=recovery", func() {
		Expect(phonehome.InstallBundledExtensionForTest(
			phonehome.BundledExtension{Type: "sysext", Name: "rescue-tools", Source: "https://x/r"},
			"recovery",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/r"},
			{"kairos-agent", "sysext", "enable", "--recovery", "rescue-tools"},
		}))
	})
})

var _ = Describe("handleUpgrade — extensions bundle", func() {
	var rec *commandRecorder
	var restoreExec, restoreRoot, restoreReboot func()
	var rebootCalled bool

	BeforeEach(func() {
		tmpRoot := GinkgoT().TempDir()
		restoreRoot = phonehome.SetExtensionsPersistentRoot(func(extType string) string {
			return filepath.Join(tmpRoot, extType+"s")
		})
		rec = &commandRecorder{}
		restoreExec = phonehome.SetExecCommand(rec.record)
		rebootCalled = false
		restoreReboot = phonehome.SetScheduleReboot(func() { rebootCalled = true })
	})
	AfterEach(func() {
		restoreExec()
		restoreRoot()
		restoreReboot()
	})

	It("is a no-op for extensions when the arg is empty (backward compat)", func() {
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			ID: "u1", Command: "upgrade",
			Args: map[string]string{"source": "oci:quay.io/myorg/edge-os:v4.2.0"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(1))
		Expect(rec.calls[0]).To(ContainElement("upgrade"))
		Expect(rebootCalled).To(BeTrue())
	})

	It("installs every bundled extension before running upgrade", func() {
		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "tailscale-agent", Source: "https://x/a"},
			{Type: "confext", Name: "fluent-bit-config", Source: "https://x/b"},
		})
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": string(raw),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(5))
		Expect(rec.calls[0][1:3]).To(Equal([]string{"sysext", "install"}))
		Expect(rec.calls[1][1:3]).To(Equal([]string{"sysext", "enable"}))
		Expect(rec.calls[1]).To(ContainElement("--active"))
		Expect(rec.calls[2][1:3]).To(Equal([]string{"confext", "install"}))
		Expect(rec.calls[3][1:3]).To(Equal([]string{"confext", "enable"}))
		Expect(rec.calls[4]).To(ContainElement("upgrade"))
		Expect(rebootCalled).To(BeTrue())
	})

	It("aborts before the OS upgrade when an extension install fails", func() {
		failRec := &failingRecorder{failOn: 0}
		restoreExec()
		restoreExec = phonehome.SetExecCommand(failRec.record)

		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "x", Source: "https://x/a"},
		})
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": string(raw),
			},
		})
		Expect(err).To(MatchError(ContainSubstring("install sysext/x")))
		Expect(failRec.calls).To(HaveLen(1))
		Expect(rebootCalled).To(BeFalse())
	})

	It("enables bundled extensions at --recovery scope for upgrade-recovery", func() {
		raw, _ := json.Marshal([]phonehome.BundledExtension{
			{Type: "sysext", Name: "rescue-tools", Source: "https://x/r"},
		})
		_, err := phonehome.HandleUpgradeForTest(phonehome.CommandData{
			Command: "upgrade-recovery",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"recovery":   "true",
				"extensions": string(raw),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(3))
		Expect(rec.calls[1]).To(Equal([]string{"kairos-agent", "sysext", "enable", "--recovery", "rescue-tools"}))
	})
})
