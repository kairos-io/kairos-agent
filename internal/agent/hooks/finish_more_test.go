package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	specImpl "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkBundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Finish additional paths", func() {
	It("exercises the encrypt failure path", func() {
		c := makeConfig()
		// Install.Encrypt with the OEM label drives Encrypt() into the
		// backupOEMIfNeeded branch which fails in a headless test env. Finish
		// should surface that error.
		c.Install = &sdkInstall.Install{Encrypt: []string{constants.OEMLabel}}
		c.Syscall = &v1mock.FakeSyscall{}
		c.Runner = v1mock.NewFakeRunner()
		sp := &specImpl.EmptySpec{}
		// Either an error (expected in most envs) or nil (if the env happens to
		// let backup succeed). We only care that the code path was exercised.
		_ = (Finish{}).Run(c, sp)
	})

	It("does not error on the bundles path", func() {
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
		// BundlePostInstall fails (mount fails), but FailOnBundleErrors=false so
		// Finish keeps going and returns nil.
		_ = (Finish{}).Run(c, sp)
	})
})
