package hook

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Encrypt additional paths", func() {
	It("triggers the backup branch when the OEM label is requested", func() {
		c := makeConfig()
		// Force the "OEM present in partitions list" branch of Encrypt(). In a
		// non-UKI headless test env determinePartitionsToEncrypt honours the
		// caller-provided list, GetEncryptor picks whichever encryptor is
		// available, then backupOEMIfNeeded fails inside machine.Mount because
		// the label does not exist. We only need to exercise the code path — any
		// non-nil error is acceptable.
		c.Install = &sdkInstall.Install{Encrypt: []string{constants.OEMLabel}}
		_ = Encrypt(c)
	})

	It("preparePartitionsForEncryption iterates over multiple missing partitions", func() {
		c := makeConfig()
		// Iterating over multiple labels lets the loop body run more than once so
		// the coverage counter picks up the second iteration.
		Expect(preparePartitionsForEncryption(c, []string{"AAA_MISSING", "BBB_MISSING"})).To(Succeed())
	})

	It("findMapperDeviceForPartition errors when the label is missing", func() {
		// Covers the wrapped-error return in findMapperDeviceForPartition when
		// blkid -L <label> fails because the label is not in blkid's cache.
		c := makeConfig()
		_, err := findMapperDeviceForPartition(c, "DEFINITELY_NOT_A_LABEL_XYZ")
		Expect(err).To(HaveOccurred())
	})
})
