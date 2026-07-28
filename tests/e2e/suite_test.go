//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

const registryContainerName = "kairos-agent-e2e-registry"

// Shared across specs. regPort is the host port the registry is published on;
// sourceRepo is the in-registry repo path (e.g. "kairos/source:test").
// suiteVM is a single live-ISO VM reused by every spec so we pay the boot cost
// once for the whole suite instead of per-It.
var (
	regPort    int
	sourceRepo string
	suiteVM    VM
)

// baseImage is the Hadron release OCI image used both as the registry source
// and (separately, at ISO build time) as the ISO base. Override with BASE_IMAGE.
func baseImage() string {
	if v := os.Getenv("BASE_IMAGE"); v != "" {
		return v
	}
	return "ghcr.io/kairos-io/hadron:v0.3.0" // non-kairosified Hadron base; kairos-init runs over it at ISO build time
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "kairos-agent e2e suite")
}

// BeforeSuite / AfterSuite are used instead of the Synchronized variants
// because the suite pins a single VM that cannot be shared across ginkgo
// parallel processes. If we later want ginkgo parallelism, switch back to
// SynchronizedBeforeSuite and boot one VM per node.
var _ = BeforeSuite(func() {
	port, err := freePort()
	Expect(err).ToNot(HaveOccurred())
	repo, err := startRegistry(registryContainerName, baseImage(), port)
	Expect(err).ToNot(HaveOccurred())
	regPort = port
	sourceRepo = repo

	suiteVM = startVM()
	suiteVM.EventuallyConnects(sshTimeout())
})

var _ = AfterSuite(func() {
	if suiteVM.StateDir != "" {
		_ = suiteVM.Destroy(nil)
	}
	_ = stopRegistry(registryContainerName)
})

// sourceURI builds the oci: source the guest uses to reach the host registry.
// 10.0.2.2 is the QEMU user-net host gateway; .sslip.io makes ggcr default to
// HTTPS (not RFC1918 auto-HTTP), so --allow-insecure-registries actually matters.
func sourceURI() string {
	return "oci:10.0.2.2.sslip.io:" + strconv.Itoa(regPort) + "/" + sourceRepo
}

// dumpSuiteSerialOnFailure records the serial log for the shared VM whenever a
// spec fails; helper wraps the shared-VM contract in one place.
func dumpSuiteSerialOnFailure() {
	if CurrentSpecReport().Failed() {
		fmt.Println("Spec failed — dumping serial log for shared VM")
		dumpSerial(suiteVM)
	}
}
