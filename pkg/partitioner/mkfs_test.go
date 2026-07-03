package partitioner_test

import (
	"testing"

	"github.com/kairos-io/kairos-agent/v2/pkg/partitioner"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMkfs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mkfs Suite")
}

var _ = Describe("MkfsCall", func() {
	It("builds ext4 options with a label", func() {
		runner := v1mock.NewFakeRunner()
		mkfs := partitioner.NewMkfsCall("/dev/sda1", "ext4", "COS_STATE", runner, "-F")
		_, err := mkfs.Apply()
		Expect(err).ToNot(HaveOccurred())
		Expect(runner.IncludesCmds([][]string{{"mkfs.ext4", "-L", "COS_STATE", "-F", "/dev/sda1"}})).To(Succeed())
	})

	It("builds vfat options with a label", func() {
		runner := v1mock.NewFakeRunner()
		mkfs := partitioner.NewMkfsCall("/dev/sda1", "vfat", "COS_GRUB", runner)
		_, err := mkfs.Apply()
		Expect(err).ToNot(HaveOccurred())
		Expect(runner.IncludesCmds([][]string{{"mkfs.vfat", "-n", "COS_GRUB", "/dev/sda1"}})).To(Succeed())
	})

	It("rejects unsupported filesystems", func() {
		mkfs := partitioner.NewMkfsCall("/dev/sda1", "btrfs", "label", v1mock.NewFakeRunner())
		_, err := mkfs.Apply()
		Expect(err).To(MatchError(ContainSubstring("unsupported filesystem")))
	})
})
