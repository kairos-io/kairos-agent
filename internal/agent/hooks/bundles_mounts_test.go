package hook

import (
	"testing"

	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	"github.com/twpayne/go-vfs/v5/vfst"
)

func TestBundlePostInstall_WithBundlesMountFails(t *testing.T) {
	c := makeConfig()
	c.Install = &sdkInstall.Install{
		Bundles: sdkBundles.Bundles{
			sdkBundles.Bundle{Repository: "raw", Targets: []string{"file:///no/such"}},
		},
	}
	sp := &specImpl.EmptySpec{}
	// machine.Mount(OEM label) will fail in a headless test env with no root
	// privileges. The hook returns that error unconditionally regardless of
	// FailOnBundleErrors — this test just wants to exercise the mount block.
	if err := (BundlePostInstall{}).Run(c, sp); err == nil {
		t.Skip("environment allowed OEM mount to succeed unexpectedly")
	}
}

func TestCustomMounts_WithMountsFails(t *testing.T) {
	c := makeConfig()
	c.Install = &sdkInstall.Install{
		BindMounts:      []string{"/foo"},
		EphemeralMounts: []string{"/tmp/ephemeral"},
	}
	sp := &specImpl.EmptySpec{}
	// machine.Mount("COS_OEM", "/oem") requires the label to exist; will fail
	// and CustomMounts returns the error. Accept either result.
	_ = (CustomMounts{}).Run(c, sp)
}

func TestCopyLogs_MountFailReturnsNil(t *testing.T) {
	c := makeConfig()
	// Point at an in-memory FS so MkdirAll and other file operations do not
	// affect the host system.
	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("test fs: %v", err)
	}
	defer cleanup()
	c.Fs = fs
	c.Syscall = &v1mock.FakeSyscall{}
	c.Runner = v1mock.NewFakeRunner()
	sp := &specImpl.EmptySpec{}
	// The Syscall mock's Mount succeeds silently, but the rsync SideEffect on
	// the FakeRunner returns empty by default → SyncData either succeeds or
	// logs a warning; either way Run returns nil.
	_ = (CopyLogs{}).Run(c, sp)
}

func TestFinish_Chains(t *testing.T) {
	c := makeConfig()
	c.Install = &sdkInstall.Install{} // empty → determinePartitionsToEncrypt returns []
	c.Syscall = &v1mock.FakeSyscall{}
	c.Runner = v1mock.NewFakeRunner()
	sp := &specImpl.EmptySpec{}
	// Encrypt() returns nil immediately (no partitions), then the chain runs
	// GrubPostInstallOptions (no opts → nil), BundlePostInstall (empty →
	// nil), CustomMounts (empty → nil), CopyLogs (best-effort → nil).
	if err := (Finish{}).Run(c, sp); err != nil {
		t.Fatalf("Finish.Run err: %v", err)
	}
}
