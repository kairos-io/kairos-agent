package agent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getConfig", func() {
	It("returns a config for valid args", func() {
		// getConfig funnels the CLI args through generateUpgradeConfForCLIArgs
		// and then config.Scan. The happy path is exercised here to cover the
		// success branch — the caller (upgrade / upgradeUki) is exercised by
		// other tests. Note: we intentionally do not exercise the full upgrade()
		// flow with a nousers cloud-config here because NewUpgradeSpec panics
		// with a nil-deref when it reaches setUpgradeSourceSize in a test env
		// with no state/recovery partitions (upstream bug — see pkg/config
		// spec.go GetSourceSize).
		dir := GinkgoT().TempDir()
		cfg, err := getConfig("oci:foo:tag", []string{dir}, "some-entry", false, true, "/x")
		Expect(err).ToNot(HaveOccurred(), "getConfig err")
		Expect(cfg).ToNot(BeNil(), "expected non-nil config")
	})
})
