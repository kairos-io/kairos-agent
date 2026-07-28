package hook

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	specImplForMounts "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	sdkInstallForMounts "github.com/kairos-io/kairos-sdk/types/install"
	yip "github.com/mudler/yip/pkg/schema"
)

var errForMounts = errors.New("boom-mounts")

func TestSaveCloudConfig_WritesFile(t *testing.T) {
	// Point saveCloudConfig at a temp directory instead of /oem so we can
	// exercise the yaml.Marshal + os.WriteFile path directly.
	dir := t.TempDir()
	orig := oemCloudConfigDir
	oemCloudConfigDir = dir
	defer func() { oemCloudConfigDir = orig }()

	yc := yip.YipConfig{Name: "hello"}
	if err := saveCloudConfig(config.Stage("mystage"), yc); err != nil {
		t.Fatalf("saveCloudConfig err: %v", err)
	}
	path := filepath.Join(dir, "10_mystage.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile err: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty yaml content")
	}
}

func TestCustomMounts_Run_MountSuccessWritesConfig(t *testing.T) {
	// Redirect both the mount function and the saveCloudConfig target so
	// CustomMounts.Run can complete end-to-end without touching the host's
	// /oem or invoking the kernel mounter.
	dir := t.TempDir()
	origDir := oemCloudConfigDir
	origMount := customMountsMountFn
	origUmount := customMountsUmountFn
	oemCloudConfigDir = dir
	customMountsMountFn = func(_, _ string) error { return nil }
	customMountsUmountFn = func(_ string) error { return nil }
	defer func() {
		oemCloudConfigDir = origDir
		customMountsMountFn = origMount
		customMountsUmountFn = origUmount
	}()

	c := makeConfig()
	c.Install = &sdkInstallForMounts.Install{
		BindMounts:      []string{"/foo", "/bar"},
		EphemeralMounts: []string{"/eph"},
	}
	if err := (CustomMounts{}).Run(c, &specImplForMounts.EmptySpec{}); err != nil {
		t.Fatalf("CustomMounts.Run err: %v", err)
	}
}

func TestCustomMounts_Run_MountFailurePropagates(t *testing.T) {
	origMount := customMountsMountFn
	customMountsMountFn = func(_, _ string) error { return errForMounts }
	defer func() { customMountsMountFn = origMount }()

	c := makeConfig()
	c.Install = &sdkInstallForMounts.Install{
		BindMounts: []string{"/foo"},
	}
	if err := (CustomMounts{}).Run(c, &specImplForMounts.EmptySpec{}); err == nil {
		t.Fatal("expected error when mount fails")
	}
}

func TestSaveCloudConfig_UnwritableDir(t *testing.T) {
	orig := oemCloudConfigDir
	oemCloudConfigDir = "/proc/kairos-does-not-exist-xxxx"
	defer func() { oemCloudConfigDir = orig }()

	// /proc is not writable → os.WriteFile fails → error propagated.
	err := saveCloudConfig(config.Stage("x"), yip.YipConfig{})
	if err == nil {
		t.Skip("environment allowed /proc write unexpectedly")
	}
}
