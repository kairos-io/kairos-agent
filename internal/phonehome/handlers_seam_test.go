package phonehome_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The specs in this file exercise the paths that hit /oem and
// exec.Command("kairos-agent",...) — production code paths that are
// otherwise untestable on a dev host. They rely on the SetOEMSeams and
// SetKairosAgentUpgrade helpers to redirect the writes and stub the exec.

// applyCloudConfigViaHandler drives the internal handleApplyCloudConfig
// through the public DefaultCommandHandler with an "allow apply-cloud-config"
// policy — that's the only door the internal package exposes for that path.
func applyCloudConfigViaHandler(cmd phonehome.CommandData) (string, error) {
	handler := phonehome.DefaultCommandHandler("http://example",
		func() string { return "" },
		func(c string) bool { return c == "apply-cloud-config" },
		nil, nil)
	return handler(cmd)
}

var _ = Describe("writeOEMCloudConfig via the command handler", func() {
	It("writes the file and prepends the cloud-config header", func() {
		dir := GinkgoT().TempDir()
		restore := phonehome.SetOEMSeams(dir, func() error { return nil })
		defer restore()

		stopReboot := phonehome.SetRebootScheduler(func() {})
		defer stopReboot()

		// The handler prepends "#cloud-config" when it's missing.
		_, err := applyCloudConfigViaHandler(phonehome.CommandData{
			Command: "apply-cloud-config",
			Args:    map[string]string{"config": "users:\n  - name: foo\n"},
		})
		Expect(err).ToNot(HaveOccurred())

		data, err := os.ReadFile(filepath.Join(dir, "99_phonehome_remote.yaml"))
		Expect(err).ToNot(HaveOccurred())
		got := string(data)
		Expect(got).To(HavePrefix("#cloud-config\n"), "missing header")
		Expect(got).To(ContainSubstring("name: foo"), "payload missing")
	})

	It("preserves an existing cloud-config header", func() {
		dir := GinkgoT().TempDir()
		restore := phonehome.SetOEMSeams(dir, func() error { return nil })
		defer restore()

		original := "#cloud-config\nhostname: bar\n"
		_, err := applyCloudConfigViaHandler(phonehome.CommandData{
			Command: "apply-cloud-config",
			Args:    map[string]string{"config": original},
		})
		Expect(err).ToNot(HaveOccurred())
		data, err := os.ReadFile(filepath.Join(dir, "99_phonehome_remote.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal(original), "expected original preserved")
		// Also ensures no double-prepended header.
		Expect(strings.Count(string(data), "#cloud-config")).To(Equal(1), "header duplicated")
	})

	It("returns the write error when the OEM directory is unusable", func() {
		// Point the OEM dir at a path that cannot be created (a regular file where
		// a dir is expected). MkdirAll will fail (logged as a warning), and the
		// subsequent os.WriteFile call must surface the underlying error.
		tmp := GinkgoT().TempDir()
		blocker := filepath.Join(tmp, "notadir")
		Expect(os.WriteFile(blocker, []byte("x"), 0600)).To(Succeed())
		restore := phonehome.SetOEMSeams(blocker, func() error { return nil })
		defer restore()

		_, err := applyCloudConfigViaHandler(phonehome.CommandData{
			Command: "apply-cloud-config",
			Args:    map[string]string{"config": "hostname: x"},
		})
		Expect(err).To(HaveOccurred(), "expected write error")
	})

	It("fails when the config argument is missing", func() {
		handler := phonehome.DefaultCommandHandler("http://example",
			func() string { return "" },
			func(c string) bool { return c == "apply-cloud-config" },
			nil, nil)
		_, err := handler(phonehome.CommandData{Command: "apply-cloud-config", Args: map[string]string{}})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("handleUpgrade via the kairos-agent seam", func() {
	It("runs the upgrade and schedules a reboot on success", func() {
		var gotArgs []string
		restoreExec := phonehome.SetKairosAgentUpgrade(func(_ context.Context, args ...string) ([]byte, error) {
			gotArgs = args
			return []byte("upgrade ok"), nil
		})
		defer restoreExec()

		rebooted := make(chan struct{}, 1)
		restoreReboot := phonehome.SetRebootScheduler(func() { rebooted <- struct{}{} })
		defer restoreReboot()

		cfg := &phonehome.Config{}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

		// Direct source without any ":" → prefixed with "oci:".
		out, err := handler(phonehome.CommandData{
			Command: "upgrade",
			Args:    map[string]string{"source": "someref"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("upgrade ok"), "output missing")
		Expect(len(gotArgs)).To(BeNumerically(">=", 3), "unexpected args: %v", gotArgs)
		Expect(gotArgs[0]).To(Equal("upgrade"))
		Expect(gotArgs[1]).To(Equal("--source"))
		Expect(gotArgs[2]).To(Equal("oci:someref"))
		Expect(rebooted).To(Receive(), "expected reboot to be scheduled after upgrade")
	})

	It("passes --recovery and does not reboot for upgrade-recovery", func() {
		restoreExec := phonehome.SetKairosAgentUpgrade(func(_ context.Context, args ...string) ([]byte, error) {
			// Verify --recovery is passed.
			for _, a := range args {
				if a == "--recovery" {
					return []byte("done"), nil
				}
			}
			return nil, errors.New("missing --recovery")
		})
		defer restoreExec()

		rebooted := false
		restoreReboot := phonehome.SetRebootScheduler(func() { rebooted = true })
		defer restoreReboot()

		cfg := &phonehome.Config{}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

		_, err := handler(phonehome.CommandData{
			Command: "upgrade-recovery",
			Args:    map[string]string{"source": "oci:foo"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rebooted).To(BeFalse(), "upgrade-recovery must not schedule a reboot")
	})

	It("surfaces the exec error and does not reboot on failure", func() {
		restoreExec := phonehome.SetKairosAgentUpgrade(func(_ context.Context, args ...string) ([]byte, error) {
			return []byte("boom"), errors.New("exit 1")
		})
		defer restoreExec()

		rebooted := false
		restoreReboot := phonehome.SetRebootScheduler(func() { rebooted = true })
		defer restoreReboot()

		cfg := &phonehome.Config{}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

		out, err := handler(phonehome.CommandData{
			Command: "upgrade",
			Args:    map[string]string{"source": "oci:foo"},
		})
		Expect(err).To(HaveOccurred(), "expected exec error to surface")
		Expect(out).To(ContainSubstring("boom"), "output missing")
		Expect(rebooted).To(BeFalse(), "reboot must not fire on failure")
	})
})

var _ = Describe("DefaultCommandHandler dispatch", func() {
	It("schedules a reboot for the reboot command", func() {
		rebooted := false
		restore := phonehome.SetRebootScheduler(func() { rebooted = true })
		defer restore()

		cfg := &phonehome.Config{}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)
		msg, err := handler(phonehome.CommandData{Command: "reboot"})
		Expect(err).ToNot(HaveOccurred())
		Expect(msg).To(ContainSubstring("Rebooting"), "unexpected msg")
		Expect(rebooted).To(BeTrue(), "reboot not scheduled")
	})

	It("fails when exec is missing the command argument", func() {
		// exec is not in defaults; opt it in to hit the "missing command arg" branch.
		cfg := &phonehome.Config{AllowedCommands: []string{"exec"}}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)
		_, err := handler(phonehome.CommandData{Command: "exec", Args: map[string]string{}})
		Expect(err).To(HaveOccurred(), "expected error for missing exec command arg")
	})

	It("runs an exec command through the shell", func() {
		// A trivial `echo` will complete instantly and stays hermetic.
		cfg := &phonehome.Config{AllowedCommands: []string{"exec"}}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)
		out, err := handler(phonehome.CommandData{Command: "exec", Args: map[string]string{"command": "echo kairos"}})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("kairos"), "unexpected output")
	})

	It("rejects unknown commands", func() {
		// Fake allow-anything so the gate lets the unknown command through to the
		// default case in the switch.
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, func(string) bool { return true }, nil, nil)
		_, err := handler(phonehome.CommandData{Command: "wat"})
		Expect(err).To(MatchError(ContainSubstring("unknown command")))
	})
})
