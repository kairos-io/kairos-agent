package phonehome

import (
	"errors"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("handleReset", func() {
	var (
		originalSelectBootEntry = selectBootEntry
		originalRebootScheduler = rebootScheduler
	)

	AfterEach(func() {
		selectBootEntry = originalSelectBootEntry
		rebootScheduler = originalRebootScheduler
	})

	It("fails without the scanned system configuration", func() {
		_, err := handleReset(nil)

		Expect(err).To(MatchError(ContainSubstring("scanned system configuration")))
	})

	It("selects the automatic state-reset entry before scheduling a reboot", func() {
		cfg := &sdkConfig.Config{}
		selected := ""
		rebootScheduled := false
		selectBootEntry = func(actualConfig *sdkConfig.Config, entry string) error {
			Expect(actualConfig).To(BeIdenticalTo(cfg))
			selected = entry
			return nil
		}
		rebootScheduler = func() { rebootScheduled = true }

		message, err := handleReset(cfg)

		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal("statereset"))
		Expect(rebootScheduled).To(BeTrue())
		Expect(message).To(ContainSubstring("Rebooting"))
	})

	It("is dispatched by the default command handler", func() {
		cfg := &sdkConfig.Config{}
		selectBootEntry = func(*sdkConfig.Config, string) error { return nil }
		rebootScheduler = func() {}
		handler := DefaultCommandHandler("http://example", func() string { return "" }, func(command string) bool {
			return command == "reset"
		}, nil, cfg)

		message, err := handler(CommandData{Command: "reset"})

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(ContainSubstring("Automatic state reset selected"))
	})

	It("does not reboot when selecting the state-reset entry fails", func() {
		selectBootEntry = func(*sdkConfig.Config, string) error { return errors.New("selection failed") }
		rebootScheduler = func() { Fail("reboot must not be scheduled") }

		_, err := handleReset(&sdkConfig.Config{})

		Expect(err).To(MatchError(ContainSubstring("selection failed")))
	})
})
