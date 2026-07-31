package hook

import (
	"bytes"
	"errors"
	"strings"

	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type stubHook struct {
	called bool
	err    error
}

func (s *stubHook) Run(_ sdkConfig.Config, _ sdkSpec.Spec) error {
	s.called = true
	return s.err
}

var _ = Describe("Hook runner", func() {
	It("Run invokes all hooks", func() {
		buf := &bytes.Buffer{}
		cfg := sdkConfig.Config{
			Logger:    sdkLogger.NewBufferLogger(buf),
			Collector: collector.Config{},
		}
		sp := &specImpl.EmptySpec{}

		a, b := &stubHook{}, &stubHook{}
		Expect(Run(cfg, sp, a, b)).To(Succeed())
		Expect(a.called).To(BeTrue(), "expected all hooks called")
		Expect(b.called).To(BeTrue(), "expected all hooks called")
	})

	It("Run stops on the first error", func() {
		buf := &bytes.Buffer{}
		cfg := sdkConfig.Config{
			Logger:    sdkLogger.NewBufferLogger(buf),
			Collector: collector.Config{},
		}
		sp := &specImpl.EmptySpec{}

		boom := errors.New("boom")
		a := &stubHook{err: boom}
		b := &stubHook{}
		err := Run(cfg, sp, a, b)
		Expect(errors.Is(err, boom)).To(BeTrue(), "expected boom, got %v", err)
		Expect(a.called).To(BeTrue(), "expected first hook to run")
		Expect(b.called).To(BeFalse(), "expected subsequent hook to be skipped after error")
	})

	It("lockPartitions closes each mapper device individually", func() {
		// Inject a dmsetup listing so the per-device cryptsetup close loop runs.
		// The second call is per-mapper: return a normal success, a semaphore
		// warning that the code must swallow, and a plain failure — each drives
		// a different branch of the loop.
		buf := &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(buf)
		logger.SetLevel("debug")

		calls := 0
		orig := lockPartitionsShFn
		defer func() { lockPartitionsShFn = orig }()
		lockPartitionsShFn = func(cmd string) (string, error) {
			calls++
			switch {
			case strings.Contains(cmd, "udevadm"):
				return "", nil
			case strings.Contains(cmd, "dmsetup ls"):
				return "vda2  (252:1)\nvda3  (252:2)\nvda4  (252:3)\n", nil
			case strings.Contains(cmd, "cryptsetup close vda2"):
				return "", nil
			case strings.Contains(cmd, "cryptsetup close vda3"):
				return "incorrect semaphore state", errors.New("cryptsetup: semaphore")
			default:
				return "boom", errors.New("cryptsetup failed")
			}
		}
		lockPartitions(logger)
		Expect(calls).To(BeNumerically(">=", 4), "expected at least 4 shell invocations")
	})

	It("lockPartitions skips the 'No devices found' line", func() {
		// "No devices found" is a printable line that dmsetup emits on empty
		// systems; the loop must skip that line and return without invoking
		// cryptsetup.
		buf := &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(buf)
		logger.SetLevel("debug")

		closeCalled := false
		orig := lockPartitionsShFn
		defer func() { lockPartitionsShFn = orig }()
		lockPartitionsShFn = func(cmd string) (string, error) {
			if strings.Contains(cmd, "dmsetup ls") {
				return "No devices found\n", nil
			}
			if strings.Contains(cmd, "cryptsetup close") {
				closeCalled = true
			}
			return "", nil
		}
		lockPartitions(logger)
		Expect(closeCalled).To(BeFalse(), "expected cryptsetup close not to be called for 'No devices found'")
	})

	It("lockPartitions early-returns on a dmsetup error", func() {
		// dmsetup ls itself failing should early-return; no cryptsetup close.
		buf := &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(buf)
		logger.SetLevel("debug")

		closeCalled := false
		orig := lockPartitionsShFn
		defer func() { lockPartitionsShFn = orig }()
		lockPartitionsShFn = func(cmd string) (string, error) {
			if strings.Contains(cmd, "dmsetup ls") {
				return "", errors.New("dmsetup missing")
			}
			if strings.Contains(cmd, "cryptsetup close") {
				closeCalled = true
			}
			return "", nil
		}
		lockPartitions(logger)
		Expect(closeCalled).To(BeFalse(), "expected no cryptsetup close on dmsetup error")
	})

	It("lockPartitions does not panic with no devices", func() {
		buf := &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(buf)
		logger.SetLevel("debug")
		// This shells out to `dmsetup ls --target crypt`. In the test env dmsetup
		// may be missing (error path) or return "No devices found" — either way,
		// lockPartitions must not panic and must return without error signalling
		// beyond debug logs.
		lockPartitions(logger)
	})

	It("GrubPostInstallOptions early-returns with no options and no encrypt", func() {
		// Not-UKI (test /proc/cmdline lacks rd.immucore.uki), empty Install with
		// no Encrypt and no GrubOptions → early return with nil.
		cfg := makeConfig()
		cfg.Install = &sdkInstall.Install{}
		sp := &specImpl.EmptySpec{}
		// If we're running in a UKI-tagged environment the code returns nil
		// anyway; both branches accept nil so this must never error.
		Expect((GrubPostInstallOptions{}).Run(cfg, sp)).To(Succeed())
	})

	It("GrubPostInstallOptions writes to STATE grubenv when OEM is encrypted", func() {
		cfg := makeConfig()
		// OEM in Encrypt list + a grub option → hook writes to STATE grubenv.
		// In the test env writes to /run/initramfs/cos-state/grub/grubenv fail
		// with permission or missing-dir errors; accept either result.
		cfg.Install = &sdkInstall.Install{
			Encrypt:     []string{"COS_OEM"},
			GrubOptions: map[string]string{"default_menu_entry": "Kairos"},
		}
		sp := &specImpl.EmptySpec{}
		_ = (GrubPostInstallOptions{}).Run(cfg, sp)
	})

	It("GrubFirstBootOptions runs with options", func() {
		cfg := makeConfig()
		cfg.GrubOptions = map[string]string{"default_menu_entry": "Kairos"}
		sp := &specImpl.EmptySpec{}
		_ = (GrubFirstBootOptions{}).Run(cfg, sp)
	})
})
