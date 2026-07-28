package hook

import (
	"testing"
	"time"

	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
)

// rebootableSpec is a small sdkSpec.Spec impl that reports the reboot /
// shutdown intents we need for the lifecycle tests. EmptySpec always returns
// false for both, so we can't use it here.
type rebootableSpec struct {
	specImpl.InstallSpec
	reboot, shutdown bool
}

func (r *rebootableSpec) ShouldReboot() bool   { return r.reboot }
func (r *rebootableSpec) ShouldShutdown() bool { return r.shutdown }

func TestLifecycle_Run_RebootBranch(t *testing.T) {
	rebooted := false
	origSleep := lifecycleSleepFn
	origReboot := lifecycleRebootFn
	lifecycleSleepFn = func(_ time.Duration) {}
	lifecycleRebootFn = func() { rebooted = true }
	defer func() {
		lifecycleSleepFn = origSleep
		lifecycleRebootFn = origReboot
	}()

	cfg := makeConfig()
	sp := &rebootableSpec{reboot: true}
	if err := (Lifecycle{}).Run(cfg, sp); err != nil {
		t.Fatalf("Lifecycle.Run err: %v", err)
	}
	if !rebooted {
		t.Fatal("expected Reboot to be called")
	}
}

func TestLifecycle_Run_ShutdownBranch(t *testing.T) {
	off := false
	origSleep := lifecycleSleepFn
	origOff := lifecyclePowerOffFn
	lifecycleSleepFn = func(_ time.Duration) {}
	lifecyclePowerOffFn = func() { off = true }
	defer func() {
		lifecycleSleepFn = origSleep
		lifecyclePowerOffFn = origOff
	}()

	cfg := makeConfig()
	sp := &rebootableSpec{shutdown: true}
	if err := (Lifecycle{}).Run(cfg, sp); err != nil {
		t.Fatalf("Lifecycle.Run err: %v", err)
	}
	if !off {
		t.Fatal("expected PowerOFF to be called")
	}
}
