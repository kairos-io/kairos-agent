package agent

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

var _ = Describe("InteractiveInstall", func() {
	It("errors when no installer binary is found", func() {
		// Ensure the env override is unset so Resolve falls back to disk paths.
		GinkgoT().Setenv("KAIROS_INSTALLER", "")

		logger := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		err := InteractiveInstall(false, "", logger)
		if err == nil {
			Skip("an installer is present on this host, cannot test the not-found branch")
		}
		Expect(err.Error()).To(ContainSubstring("no interactive installer found"), "unexpected error: %v", err)
	})
})
