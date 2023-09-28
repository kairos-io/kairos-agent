package agent

import (
	"fmt"
	"os"
	"path"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("generateUpgradeSpec", func() {
	When("there are command line arguments", func() {
		var configDir string
		var upgradeRecovery bool

		BeforeEach(func() {
			upgradeRecovery = false
			configDir, err := os.MkdirTemp("", "upgrade-test")
			Expect(err).ToNot(HaveOccurred())

			config := fmt.Sprintf(`upgrade:
  recovery: %t
  recovery-system:
    uri: oci://image-in-conf
`, upgradeRecovery)

			configFilePath := path.Join(configDir, "config.yaml")
			err = os.WriteFile(configFilePath, []byte(config), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			os.RemoveAll(configDir)
		})

		It("overrides kairos config yaml values", func() {
			spec, _, err := generateUpgradeSpec("", "oci:myimage", false, false,
				[]string{}, false, !upgradeRecovery)

			Expect(err).ToNot(HaveOccurred())
			Expect(spec.Active.Source.String()).To(Equal("oci://myimage:latest"))
			Expect(spec.Recovery.Source.String()).To(Equal("oci://myimage:latest"))
			Expect(spec.RecoveryUpgrade).To(Equal(!upgradeRecovery))
		})
	})
})
