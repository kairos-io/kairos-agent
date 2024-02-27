package action

import (
	"bytes"
	"fmt"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
	"os"
	"path/filepath"
)

var _ = Describe("Common action tests", func() {
	Describe("createExtraDirsInRootfs", func() {
		var config *agentConfig.Config
		var fs vfs.FS
		var logger sdkTypes.KairosLogger
		var runner *v1mock.FakeRunner
		var mounter *v1mock.ErrorMounter
		var syscall *v1mock.FakeSyscall
		var client *v1mock.FakeHTTPClient
		var cloudInit *v1mock.FakeCloudInitRunner
		var cleanup func()
		var memLog *bytes.Buffer
		var extractor *v1mock.FakeImageExtractor

		BeforeEach(func() {
			runner = v1mock.NewFakeRunner()
			syscall = &v1mock.FakeSyscall{}
			mounter = v1mock.NewErrorMounter()
			client = &v1mock.FakeHTTPClient{}
			memLog = &bytes.Buffer{}
			logger = sdkTypes.NewBufferLogger(memLog)
			extractor = v1mock.NewFakeImageExtractor(logger)
			logger.SetLevel("debug")
			var err error
			fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
			Expect(err).Should(BeNil())

			cloudInit = &v1mock.FakeCloudInitRunner{}
			config = agentConfig.NewConfig(
				agentConfig.WithFs(fs),
				agentConfig.WithRunner(runner),
				agentConfig.WithLogger(logger),
				agentConfig.WithMounter(mounter),
				agentConfig.WithSyscall(syscall),
				agentConfig.WithClient(client),
				agentConfig.WithCloudInitRunner(cloudInit),
				agentConfig.WithImageExtractor(extractor),
			)
			config.Install = &agentConfig.Install{}
			config.Bundles = agentConfig.Bundles{}
			config.Config = collector.Config{}
		})

		AfterEach(func() {
			cleanup()
		})

		It("creates the dirs", func() {
			extraDirs := []string{"one", "/two"}
			rootDir := "/"
			createExtraDirsInRootfs(config, extraDirs, rootDir)
			for _, d := range extraDirs {
				Expect(memLog).To(ContainSubstring(fmt.Sprintf("Creating extra dir %s under %s", d, rootDir)))
				stat, err := fs.Stat(filepath.Join(rootDir, d))
				Expect(err).ToNot(HaveOccurred())
				Expect(stat.IsDir()).To(BeTrue())
			}
		})

		It("doesnt create the dirs if they already exists", func() {
			extraDirs := []string{"one", "/two"}
			rootDir := "/"
			for _, d := range extraDirs {
				err := fs.Mkdir(filepath.Join(rootDir, d), os.ModeDir)
				Expect(err).ToNot(HaveOccurred())
				stat, err := fs.Stat(filepath.Join(rootDir, d))
				Expect(err).ToNot(HaveOccurred())
				Expect(stat.IsDir()).To(BeTrue())
			}
			createExtraDirsInRootfs(config, extraDirs, rootDir)
			for _, d := range extraDirs {
				// No message of creation
				Expect(memLog).ToNot(ContainSubstring(fmt.Sprintf("Creating extra dir %s under %s", d, rootDir)))
			}
		})

		It("Crates dirs with subdirs", func() {
			extraDirs := []string{"/one/two/", "/three/four"}
			rootDir := "/"
			createExtraDirsInRootfs(config, extraDirs, rootDir)
			for _, d := range extraDirs {
				Expect(memLog).To(ContainSubstring(fmt.Sprintf("Creating extra dir %s under %s", d, rootDir)))
				stat, err := fs.Stat(filepath.Join(rootDir, d))
				Expect(err).ToNot(HaveOccurred())
				Expect(stat.IsDir()).To(BeTrue())
			}
		})

		It("Doesnt nothing with an empty target", func() {
			extraDirs := []string{"/one/two/", "/three/four"}
			rootDir := ""
			createExtraDirsInRootfs(config, extraDirs, rootDir)
			Expect(memLog).To(ContainSubstring("Empty target for extra rootfs dirs, not doing anything"))
		})

		It("Fails with a non valid rootdir", func() {
			extraDirs := []string{"/one/two/", "/three/four"}
			rootDir := "@&^$$W#@#$"
			createExtraDirsInRootfs(config, extraDirs, rootDir)
			for _, d := range extraDirs {
				Expect(memLog).To(ContainSubstring(fmt.Sprintf("Creating extra dir %s under %s", d, rootDir)))
				Expect(memLog).To(ContainSubstring(fmt.Sprintf("Failure creating extra dir %s under %s", d, rootDir)))
				_, err := fs.Stat(filepath.Join(rootDir, d))
				Expect(err).To(HaveOccurred())
			}
		})
	})
})
