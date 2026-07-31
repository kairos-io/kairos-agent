package hook_test

import (
	"bytes"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newTestConfig builds a minimal sdkConfig.Config suitable for hooks that
// early-return before touching disks or the network.
func newTestConfig() *sdkConfig.Config {
	buf := &bytes.Buffer{}
	logger := sdkLogger.NewBufferLogger(buf)
	logger.SetLevel("debug")
	runner := v1mock.NewFakeRunner()
	cfg := config.NewConfig(
		config.WithLogger(logger),
		config.WithRunner(runner),
	)
	cfg.Collector = collector.Config{}
	return cfg
}

var _ = Describe("Hooks early-return paths", func() {
	It("Lifecycle returns nil with no reboot and no shutdown", func() {
		cfg := newTestConfig()
		sp := &specImpl.EmptySpec{}
		Expect((hook.Lifecycle{}).Run(*cfg, sp)).To(Succeed())
	})

	It("BundlePostInstall returns nil with empty bundles", func() {
		cfg := newTestConfig()
		cfg.Install = &sdkInstall.Install{}
		sp := &specImpl.EmptySpec{}
		Expect((hook.BundlePostInstall{}).Run(*cfg, sp)).To(Succeed())
	})

	It("BundleFirstBoot returns nil with empty bundles", func() {
		cfg := newTestConfig()
		sp := &specImpl.EmptySpec{}
		Expect((hook.BundleFirstBoot{}).Run(*cfg, sp)).To(Succeed())
	})

	It("CustomMounts early-returns with empty mounts", func() {
		cfg := newTestConfig()
		cfg.Install = &sdkInstall.Install{}
		sp := &specImpl.EmptySpec{}
		Expect((hook.CustomMounts{}).Run(*cfg, sp)).To(Succeed())
	})

	It("GrubFirstBootOptions returns nil with no options", func() {
		cfg := newTestConfig()
		sp := &specImpl.EmptySpec{}
		Expect((hook.GrubFirstBootOptions{}).Run(*cfg, sp)).To(Succeed())
	})

	It("BundleFirstBoot swallows bundle errors when FailOnBundleErrors is false", func() {
		cfg := newTestConfig()
		// A bundle with a non-existent repository triggers RunBundles error, but
		// because FailOnBundleErrors defaults to false, the hook must swallow it
		// and return nil.
		cfg.Bundles = sdkBundles.Bundles{
			sdkBundles.Bundle{Repository: "raw", Targets: []string{"file:///no/such/bundle.tar"}},
		}
		sp := &specImpl.EmptySpec{}
		Expect((hook.BundleFirstBoot{}).Run(*cfg, sp)).To(Succeed(),
			"BundleFirstBoot.Run expected nil since FailOnBundleErrors=false")
	})

	It("runs all FirstBoot hooks", func() {
		cfg := newTestConfig()
		cfg.Install = &sdkInstall.Install{}
		sp := &specImpl.EmptySpec{}
		// hook.FirstBoot contains BundleFirstBoot + GrubFirstBootOptions. Both
		// early-return with empty bundles / grub options. Exercise the full list.
		Expect(hook.Run(*cfg, sp, hook.FirstBoot...)).To(Succeed())
	})

	It("runs the FinishUpgrade hooks (lifecycle only)", func() {
		cfg := newTestConfig()
		sp := &specImpl.EmptySpec{}
		Expect(hook.Run(*cfg, sp, hook.FinishUpgrade...)).To(Succeed())
	})

	It("runs the FinishInstall hooks (lifecycle only)", func() {
		cfg := newTestConfig()
		sp := &specImpl.EmptySpec{}
		Expect(hook.Run(*cfg, sp, hook.FinishInstall...)).To(Succeed())
	})
})
