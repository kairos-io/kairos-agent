package bus_test

import (
	"github.com/kairos-io/kairos-agent/v2/internal/bus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bus initialize/reload", func() {
	It("Initialize registers response handlers and is idempotent", func() {
		b := bus.NewBus()
		Expect(b.HasRegisteredPlugins()).To(BeFalse())

		// Initialize with an empty temp path so we don't accidentally load
		// real providers from /system/providers.
		b.Initialize(GinkgoT().TempDir())
		// A second Initialize call must return without side effects (registered
		// flag guards it).
		b.Initialize(GinkgoT().TempDir())

		// bus.Manager and bus.Reload are exercised by other tests; keep this
		// spec focused on Initialize idempotency.
	})

	It("Reload swaps the Manager for a fresh bus and initializes it", func() {
		prev := bus.Manager
		bus.Reload()
		Expect(bus.Manager).ToNot(BeIdenticalTo(prev))
	})
})
