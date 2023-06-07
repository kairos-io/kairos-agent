package agent

import (
	"bytes"
	"context"
	"fmt"
	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos/v2/pkg/constants"
	conf "github.com/kairos-io/kairos/v2/pkg/elementalConfig"
	"github.com/kairos-io/kairos/v2/pkg/utils"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v2/pkg/config"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	v1mock "github.com/kairos-io/kairos/v2/tests/mocks"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const printOutput = `BYT;
/dev/loop0:50593792s:loopback:512:512:gpt:Loopback device:;`
const partTmpl = `
%d:%ss:%ss:2048s:ext4::type=83;`

var _ = Describe("prepareConfiguration", func() {
	path := "/foo/bar"
	url := "https://example.com"
	ctx, cancel := context.WithCancel(context.Background())

	It("returns a file path with no modifications", func() {
		source, err := prepareConfiguration(ctx, path)

		Expect(err).ToNot(HaveOccurred())
		Expect(source).To(Equal(path))
	})

	It("creates a configuration file containing the given url", func() {
		source, err := prepareConfiguration(ctx, url)

		Expect(err).ToNot(HaveOccurred())
		Expect(source).ToNot(Equal(path))

		f, err := os.Open(source)
		Expect(err).ToNot(HaveOccurred())

		var cfg config.Config
		err = yaml.NewDecoder(f).Decode(&cfg)
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.ConfigURL).To(Equal(url))
	})

	It("cleans up the configuration file after context is done", func() {
		source, err := prepareConfiguration(ctx, url)
		Expect(err).ToNot(HaveOccurred())
		cancel()

		_, err = os.Stat(source)
		Expect(os.IsNotExist(err))
	})
})

var _ = Describe("RunInstall", func() {
	var installConfig *v1.RunConfig
	var options map[string]string
	var err error
	var fs vfs.FS
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var ghwTest v1mock.GhwMock
	var cmdline func() ([]byte, error)

	BeforeEach(func() {
		// Default mock objects
		runner := v1mock.NewFakeRunner()
		syscall := &v1mock.FakeSyscall{}
		mounter := v1mock.NewErrorMounter()
		memLog = &bytes.Buffer{}
		logger := v1.NewBufferLogger(memLog)
		logger = v1.NewLogger()
		extractor := v1mock.NewFakeImageExtractor(logger)
		//logger.SetLevel(v1.DebugLevel())
		cloudInit = &v1mock.FakeCloudInitRunner{}
		// Set default cmdline function so we dont panic :o
		cmdline = func() ([]byte, error) {
			return []byte{}, nil
		}

		// Init test fs
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": ""})
		Expect(err).Should(BeNil())
		// Create tmp dir
		utils.MkdirAll(fs, "/tmp", constants.DirPerm)
		// Create grub confg
		grubCfg := filepath.Join(constants.ActiveDir, constants.GrubConf)
		err = utils.MkdirAll(fs, filepath.Dir(grubCfg), constants.DirPerm)
		Expect(err).To(BeNil())
		_, err = fs.Create(grubCfg)
		Expect(err).To(BeNil())

		// Create new runconfig with all mocked objects
		installConfig = conf.NewRunConfig(
			conf.WithFs(fs),
			conf.WithRunner(runner),
			conf.WithLogger(logger),
			conf.WithMounter(mounter),
			conf.WithSyscall(syscall),
			conf.WithCloudInitRunner(cloudInit),
			conf.WithImageExtractor(extractor),
		)

		// Side effect of runners, hijack calls to commands and return our stuff
		partNum := 0
		partedOut := printOutput
		runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
			switch cmd {
			case "parted":
				idx := 0
				for i, arg := range args {
					if arg == "mkpart" {
						idx = i
						break
					}
				}
				if idx > 0 {
					partNum++
					partedOut += fmt.Sprintf(partTmpl, partNum, args[idx+3], args[idx+4])
					_, _ = fs.Create(fmt.Sprintf("/some/device%d", partNum))
				}
				return []byte(partedOut), nil
			case "lsblk":
				return []byte(`{
"blockdevices":
    [
        {"label": "COS_ACTIVE", "type": "loop", "path": "/some/loop0"},
        {"label": "COS_OEM", "type": "part", "path": "/some/device1"},
        {"label": "COS_RECOVERY", "type": "part", "path": "/some/device2"},
        {"label": "COS_STATE", "type": "part", "path": "/some/device3"},
        {"label": "COS_PERSISTENT", "type": "part", "path": "/some/device4"}
    ]
}`), nil
			case "cat":
				if args[0] == "/proc/cmdline" {
					return cmdline()
				}
				return []byte{}, nil
			default:
				return []byte{}, nil
			}
		}

		device := "/some/device"
		err = utils.MkdirAll(fs, filepath.Dir(device), constants.DirPerm)
		Expect(err).To(BeNil())
		_, err = fs.Create(device)
		Expect(err).ShouldNot(HaveOccurred())

		userConfig := `
#cloud-config

install:
  image: test
`

		options = map[string]string{
			"device": "/some/device",
			"cc":     userConfig,
		}

		mainDisk := block.Disk{
			Name: "device",
			Partitions: []*block.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "COS_GRUB",
					Type:            "ext4",
				},
				{
					Name:            "device2",
					FilesystemLabel: "COS_STATE",
					Type:            "ext4",
				},
				{
					Name:            "device3",
					FilesystemLabel: "COS_PERSISTENT",
					Type:            "ext4",
				},
				{
					Name:            "device4",
					FilesystemLabel: "COS_ACTIVE",
					Type:            "ext4",
				},
				{
					Name:            "device5",
					FilesystemLabel: "COS_PASSIVE",
					Type:            "ext4",
				},
				{
					Name:            "device5",
					FilesystemLabel: "COS_RECOVERY",
					Type:            "ext4",
				},
				{
					Name:            "device6",
					FilesystemLabel: "COS_OEM",
					Type:            "ext4",
				},
			},
		}
		ghwTest = v1mock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
		cleanup()
	})

	It("runs the install", func() {
		Skip("Not ready yet")
		err = RunInstall(installConfig, options)
		Expect(err).ToNot(HaveOccurred())
	})
})
