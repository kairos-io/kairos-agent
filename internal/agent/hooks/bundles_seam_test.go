package hook

import (
	"testing"

	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
)

func stubBundlesMounts(t *testing.T, mountErr, umountErr error) func() {
	t.Helper()
	origM, origU := bundlesMountFn, bundlesUmountFn
	bundlesMountFn = func(_, _ string) error { return mountErr }
	bundlesUmountFn = func(_ string) error { return umountErr }
	return func() {
		bundlesMountFn = origM
		bundlesUmountFn = origU
	}
}

func TestBundlePostInstall_MountsSucceed_BundlesFailButSwallowed(t *testing.T) {
	// Both machine.Mount calls succeed via the seam. rsync + bundles.RunBundles
	// still fail (nothing to sync, unreachable bundle URL), but
	// FailOnBundleErrors=false → BundlePostInstall.Run returns nil.
	restore := stubBundlesMounts(t, nil, nil)
	defer restore()

	c := makeConfig()
	c.Install = &sdkInstall.Install{
		Bundles: sdkBundles.Bundles{
			sdkBundles.Bundle{Repository: "raw", Targets: []string{"file:///no/such/bundle.tar"}},
		},
	}
	c.FailOnBundleErrors = false
	c.Syscall = &v1mock.FakeSyscall{}
	c.Runner = v1mock.NewFakeRunner()
	sp := &specImpl.EmptySpec{}

	if err := (BundlePostInstall{}).Run(c, sp); err != nil {
		t.Fatalf("BundlePostInstall.Run err: %v", err)
	}
}

func TestBundlePostInstall_PersistentMountFails(t *testing.T) {
	// Return error on the second mount call → the persistent-mount branch
	// bails out and returns.
	origM, origU := bundlesMountFn, bundlesUmountFn
	calls := 0
	bundlesMountFn = func(_, _ string) error {
		calls++
		if calls == 2 {
			return errForMounts
		}
		return nil
	}
	bundlesUmountFn = func(_ string) error { return nil }
	defer func() { bundlesMountFn = origM; bundlesUmountFn = origU }()

	c := makeConfig()
	c.Install = &sdkInstall.Install{
		Bundles: sdkBundles.Bundles{
			sdkBundles.Bundle{Repository: "raw", Targets: []string{"file:///no/such/bundle.tar"}},
		},
	}
	c.Syscall = &v1mock.FakeSyscall{}
	c.Runner = v1mock.NewFakeRunner()
	sp := &specImpl.EmptySpec{}
	if err := (BundlePostInstall{}).Run(c, sp); err == nil {
		t.Fatal("expected error when persistent mount fails")
	}
}
