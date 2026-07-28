//go:build e2e

package e2e_test

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Strings observed in a real manual-install run. tlsError is the go-containerregistry
// failure when an HTTPS client hits a plain-HTTP registry; postPullMarker is logged
// only after the source image has been pulled and unpacked into the target, so it
// never appears in the TLS-failure path.
const (
	tlsError       = "server gave HTTP response to HTTPS client"
	postPullMarker = "Finished copying"
)

var _ = Describe("manual-install against an insecure registry", Label("insecure-registry"), func() {
	BeforeEach(func() {
		// Fresh disk for each spec — the suite-shared VM is reused, but any
		// prior install must be scrubbed so partitioning starts from zero.
		wipeDisk(suiteVM)

		cfg, err := os.ReadFile("config.yaml")
		Expect(err).ToNot(HaveOccurred())
		_, err = suiteVM.Sudo(fmt.Sprintf("cat > /tmp/config.yaml <<'EOF'\n%s\nEOF", string(cfg)))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(dumpSuiteSerialOnFailure)

	It("fails at the pull without --allow-insecure-registries", func() {
		out, err := suiteVM.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --device /dev/vda --source %s /tmp/config.yaml",
			sourceURI()))
		Expect(err).To(HaveOccurred(), out)
		Expect(out).To(ContainSubstring(tlsError), out)
	})

	It("gets past the pull with --allow-insecure-registries", func() {
		// We only verify the pull stage: with the flag the image is pulled and
		// unpacked successfully. The install may not run to completion in this
		// minimal setup, so the command error is intentionally ignored.
		out, _ := suiteVM.Sudo(fmt.Sprintf(
			"kairos-agent manual-install --allow-insecure-registries --device /dev/vda --source %s /tmp/config.yaml",
			sourceURI()))
		Expect(out).ToNot(ContainSubstring(tlsError), out)
		Expect(out).To(ContainSubstring(postPullMarker), out)
	})
})
