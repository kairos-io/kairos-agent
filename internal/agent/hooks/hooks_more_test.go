package hook_test

import (
	"bytes"
	"testing"

	hook "github.com/kairos-io/kairos-agent/v2/internal/agent/hooks"
	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
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

func TestLifecycle_NoRebootNoShutdown(t *testing.T) {
	cfg := newTestConfig()
	sp := &specImpl.EmptySpec{}
	if err := (hook.Lifecycle{}).Run(*cfg, sp); err != nil {
		t.Fatalf("Lifecycle.Run err: %v", err)
	}
}

func TestBundlePostInstall_EmptyBundles(t *testing.T) {
	cfg := newTestConfig()
	cfg.Install = &sdkInstall.Install{}
	sp := &specImpl.EmptySpec{}
	if err := (hook.BundlePostInstall{}).Run(*cfg, sp); err != nil {
		t.Fatalf("BundlePostInstall.Run err: %v", err)
	}
}

func TestBundleFirstBoot_EmptyBundles(t *testing.T) {
	cfg := newTestConfig()
	sp := &specImpl.EmptySpec{}
	if err := (hook.BundleFirstBoot{}).Run(*cfg, sp); err != nil {
		t.Fatalf("BundleFirstBoot.Run err: %v", err)
	}
}

func TestCustomMounts_EmptyMountsEarlyReturn(t *testing.T) {
	cfg := newTestConfig()
	cfg.Install = &sdkInstall.Install{}
	sp := &specImpl.EmptySpec{}
	if err := (hook.CustomMounts{}).Run(*cfg, sp); err != nil {
		t.Fatalf("CustomMounts.Run err: %v", err)
	}
}

func TestGrubFirstBootOptions_NoOptions(t *testing.T) {
	cfg := newTestConfig()
	sp := &specImpl.EmptySpec{}
	if err := (hook.GrubFirstBootOptions{}).Run(*cfg, sp); err != nil {
		t.Fatalf("GrubFirstBootOptions.Run err: %v", err)
	}
}

func TestBundleFirstBoot_WithBundles_NoFailOnBundleErrors(t *testing.T) {
	cfg := newTestConfig()
	// A bundle with a non-existent repository triggers RunBundles error, but
	// because FailOnBundleErrors defaults to false, the hook must swallow it
	// and return nil.
	cfg.Bundles = sdkBundles.Bundles{
		sdkBundles.Bundle{Repository: "raw", Targets: []string{"file:///no/such/bundle.tar"}},
	}
	sp := &specImpl.EmptySpec{}
	if err := (hook.BundleFirstBoot{}).Run(*cfg, sp); err != nil {
		t.Fatalf("BundleFirstBoot.Run err (expected nil since FailOnBundleErrors=false): %v", err)
	}
}

func TestFirstBootHooks_RunAll(t *testing.T) {
	cfg := newTestConfig()
	cfg.Install = &sdkInstall.Install{}
	sp := &specImpl.EmptySpec{}
	// hook.FirstBoot contains BundleFirstBoot + GrubFirstBootOptions. Both
	// early-return with empty bundles / grub options. Exercise the full list.
	if err := hook.Run(*cfg, sp, hook.FirstBoot...); err != nil {
		t.Fatalf("FirstBoot hooks err: %v", err)
	}
}

func TestFinishUpgradeHooks_RunLifecycleOnly(t *testing.T) {
	cfg := newTestConfig()
	sp := &specImpl.EmptySpec{}
	if err := hook.Run(*cfg, sp, hook.FinishUpgrade...); err != nil {
		t.Fatalf("FinishUpgrade err: %v", err)
	}
}

func TestFinishInstallHooks_RunLifecycleOnly(t *testing.T) {
	cfg := newTestConfig()
	sp := &specImpl.EmptySpec{}
	if err := hook.Run(*cfg, sp, hook.FinishInstall...); err != nil {
		t.Fatalf("FinishInstall err: %v", err)
	}
}
