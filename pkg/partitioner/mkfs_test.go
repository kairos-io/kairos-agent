package partitioner

import (
	"bytes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"

	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
)

func TestMkfsSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MKFS test suite")
}

var _ = Describe("MKFS", Label("mkfs"), func() {
	var logger sdkTypes.KairosLogger
	var runner *v1mock.FakeRunner
	var memLog *bytes.Buffer

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		runner = v1mock.NewFakeRunner()
	})

	Describe("FormatPartition", Label("FormatPartition", "partition", "format"), func() {
		It("Reformats an already existing partition", func() {
			Expect(FormatDevice(logger, runner, "/dev/device1", "ext4", "MY_LABEL")).To(BeNil())
		})
	})
})
