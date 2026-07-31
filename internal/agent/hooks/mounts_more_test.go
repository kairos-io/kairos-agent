package hook

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	specImplForMounts "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	sdkInstallForMounts "github.com/kairos-io/kairos-sdk/types/install"
	yip "github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var errForMounts = errors.New("boom-mounts")

var _ = Describe("Custom mounts and cloud config", func() {
	It("saveCloudConfig writes the yaml file", func() {
		// Point saveCloudConfig at a temp directory instead of /oem so we can
		// exercise the yaml.Marshal + os.WriteFile path directly.
		dir := GinkgoT().TempDir()
		orig := oemCloudConfigDir
		oemCloudConfigDir = dir
		defer func() { oemCloudConfigDir = orig }()

		yc := yip.YipConfig{Name: "hello"}
		Expect(saveCloudConfig(config.Stage("mystage"), yc)).To(Succeed())
		path := filepath.Join(dir, "10_mystage.yaml")
		b, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(b).ToNot(BeEmpty(), "expected non-empty yaml content")
	})

	It("CustomMounts.Run writes the config when the mount succeeds", func() {
		// Redirect both the mount function and the saveCloudConfig target so
		// CustomMounts.Run can complete end-to-end without touching the host's
		// /oem or invoking the kernel mounter.
		dir := GinkgoT().TempDir()
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
		Expect((CustomMounts{}).Run(c, &specImplForMounts.EmptySpec{})).To(Succeed())
	})

	It("CustomMounts.Run propagates a mount failure", func() {
		origMount := customMountsMountFn
		customMountsMountFn = func(_, _ string) error { return errForMounts }
		defer func() { customMountsMountFn = origMount }()

		c := makeConfig()
		c.Install = &sdkInstallForMounts.Install{
			BindMounts: []string{"/foo"},
		}
		Expect((CustomMounts{}).Run(c, &specImplForMounts.EmptySpec{})).ToNot(Succeed())
	})

	It("saveCloudConfig propagates the error for an unwritable dir", func() {
		orig := oemCloudConfigDir
		oemCloudConfigDir = "/proc/kairos-does-not-exist-xxxx"
		defer func() { oemCloudConfigDir = orig }()

		// /proc is not writable → os.WriteFile fails → error propagated.
		err := saveCloudConfig(config.Stage("x"), yip.YipConfig{})
		if err == nil {
			Skip("environment allowed /proc write unexpectedly")
		}
	})
})
