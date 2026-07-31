package hook

import (
	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func stubBundlesMounts(mountErr, umountErr error) func() {
	origM, origU := bundlesMountFn, bundlesUmountFn
	bundlesMountFn = func(_, _ string) error { return mountErr }
	bundlesUmountFn = func(_ string) error { return umountErr }
	return func() {
		bundlesMountFn = origM
		bundlesUmountFn = origU
	}
}

var _ = Describe("BundlePostInstall mount seams", func() {
	It("swallows bundle failures when mounts succeed and FailOnBundleErrors is false", func() {
		// Both machine.Mount calls succeed via the seam. rsync + bundles.RunBundles
		// still fail (nothing to sync, unreachable bundle URL), but
		// FailOnBundleErrors=false → BundlePostInstall.Run returns nil.
		restore := stubBundlesMounts(nil, nil)
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

		Expect((BundlePostInstall{}).Run(c, sp)).To(Succeed())
	})

	It("errors when the persistent mount fails", func() {
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
		Expect((BundlePostInstall{}).Run(c, sp)).ToNot(Succeed())
	})
})
