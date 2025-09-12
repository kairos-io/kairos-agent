package partitioner

import (
	"bytes"
	fileBackend "github.com/diskfs/go-diskfs/backend/file"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
	"os"
	"path/filepath"
	"testing"

	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
)

func TestMkfsSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MKFS test suite")
}

var _ = Describe("MKFS", Label("mkfs"), func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkTypes.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var cl *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var device, tmpdir string
	var spec *v1.InstallSpec

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		cl = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		extractor = v1mock.NewFakeImageExtractor(logger)
		logger.SetLevel("debug")
		var err error
		// create fake files needed for the loop device to "work"
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{
			"/dev/loop-control": "",
			"/dev/loop0":        "",
		})
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(cl),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
		)

		tmpdir, err = os.MkdirTemp("", "install-*")
		Expect(err).Should(BeNil())
		device = filepath.Join(tmpdir, "test.img")
		Expect(os.RemoveAll(device)).Should(Succeed())
		// at least 2Gb in size as state is set to 1G

		_, err = fileBackend.CreateFromPath(device, 2*1024*1024*1024)
		Expect(err).ToNot(HaveOccurred())
		err = fsutils.MkdirAll(fs, filepath.Dir(device), constants.DirPerm)
		Expect(err).To(BeNil())
		_, err = fs.Create(device)
		Expect(err).ShouldNot(HaveOccurred())
		config.Install = &agentConfig.Install{
			Device: device,
		}
		config.Bundles = agentConfig.Bundles{}
		config.Config = collector.Config{}
		spec, err = agentConfig.NewInstallSpec(config)
	})

	AfterEach(func() {
		cleanup()
	})
	Describe("FormatPartition", Label("FormatPartition", "partition", "format"), func() {
		It("Reformats an already existing partition", func() {
			Expect(FormatDevice(logger, runner, "/dev/device1", "ext4", "MY_LABEL")).To(BeNil())
		})
	})
	Describe("PartitionAndFormatDevice", Label("PartitionAndFormatDevice", "partition", "format"), func() {
		It("Partitions and formats a device with a GPT partition table", func() {
			// Create fake partitions
			Expect(config.Fs.Mkdir("/dev/disk", constants.DirPerm)).ToNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/dev/disk/by-partlabel", constants.DirPerm)).ToNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/dev/disk/by-partlabel/oem", constants.DirPerm)).ToNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/dev/disk/by-partlabel/recovery", constants.DirPerm)).ToNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/dev/disk/by-partlabel/state", constants.DirPerm)).ToNot(HaveOccurred())
			Expect(config.Fs.Mkdir("/dev/disk/by-partlabel/persistent", constants.DirPerm)).ToNot(HaveOccurred())
			Expect(PartitionAndFormatDevice(config, spec)).To(BeNil())
			Expect(memLog.String()).To(ContainSubstring("Partitioning device..."))
			Expect(memLog.String()).To(ContainSubstring("Formatting partition: COS_OEM"))
			Expect(memLog.String()).To(ContainSubstring("Formatting partition: COS_RECOVERY"))
			Expect(memLog.String()).To(ContainSubstring("Formatting partition: COS_STATE"))
			Expect(memLog.String()).To(ContainSubstring("Formatting partition: COS_PERSISTENT"))

		})
	})
})
