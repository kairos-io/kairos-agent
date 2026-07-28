package hook

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
)

type stubHook struct {
	called bool
	err    error
}

func (s *stubHook) Run(_ sdkConfig.Config, _ sdkSpec.Spec) error {
	s.called = true
	return s.err
}

func TestRun_AllHooks(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := sdkConfig.Config{
		Logger:    sdkLogger.NewBufferLogger(buf),
		Collector: collector.Config{},
	}
	sp := &specImpl.EmptySpec{}

	a, b := &stubHook{}, &stubHook{}
	if err := Run(cfg, sp, a, b); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if !a.called || !b.called {
		t.Fatalf("expected all hooks called: a=%v b=%v", a.called, b.called)
	}
}

func TestRun_StopsOnError(t *testing.T) {
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
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}
	if !a.called {
		t.Fatal("expected first hook to run")
	}
	if b.called {
		t.Fatal("expected subsequent hook to be skipped after error")
	}
}

func TestLockPartitions_MultipleMapperDevices(t *testing.T) {
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
	if calls < 4 {
		t.Fatalf("expected at least 4 shell invocations, got %d", calls)
	}
}

func TestLockPartitions_DmsetupNoDevicesLine(t *testing.T) {
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
	if closeCalled {
		t.Fatal("expected cryptsetup close not to be called for 'No devices found'")
	}
}

func TestLockPartitions_DmsetupError(t *testing.T) {
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
	if closeCalled {
		t.Fatal("expected no cryptsetup close on dmsetup error")
	}
}

func TestLockPartitions_NoDevices(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := sdkLogger.NewBufferLogger(buf)
	logger.SetLevel("debug")
	// This shells out to `dmsetup ls --target crypt`. In the test env dmsetup
	// may be missing (error path) or return "No devices found" — either way,
	// lockPartitions must not panic and must return without error signalling
	// beyond debug logs.
	lockPartitions(logger)
}

func TestGrubPostInstallOptions_NoOptionsNoEncrypt(t *testing.T) {
	// Not-UKI (test /proc/cmdline lacks rd.immucore.uki), empty Install with
	// no Encrypt and no GrubOptions → early return with nil.
	cfg := makeConfig()
	cfg.Install = &sdkInstall.Install{}
	sp := &specImpl.EmptySpec{}
	if err := (GrubPostInstallOptions{}).Run(cfg, sp); err != nil {
		// If we're running in a UKI-tagged environment the code returns nil
		// anyway; both branches accept nil so this must never error.
		t.Fatalf("GrubPostInstallOptions.Run err: %v", err)
	}
}

func TestGrubPostInstallOptions_OemEncryptedWithOptions(t *testing.T) {
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
}

func TestGrubFirstBootOptions_WithOptions(t *testing.T) {
	cfg := makeConfig()
	cfg.GrubOptions = map[string]string{"default_menu_entry": "Kairos"}
	sp := &specImpl.EmptySpec{}
	_ = (GrubFirstBootOptions{}).Run(cfg, sp)
}
