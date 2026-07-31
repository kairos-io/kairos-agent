package hook

import (
	"time"

	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("Lifecycle seams", func() {
	It("takes the reboot branch", func() {
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
		Expect((Lifecycle{}).Run(cfg, sp)).To(Succeed())
		Expect(rebooted).To(BeTrue(), "expected Reboot to be called")
	})

	It("takes the shutdown branch", func() {
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
		Expect((Lifecycle{}).Run(cfg, sp)).To(Succeed())
		Expect(off).To(BeTrue(), "expected PowerOFF to be called")
	})
})
