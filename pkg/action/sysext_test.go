package action_test

import (
	"bytes"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Sysext Actions test", Focus, func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkTypes.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var err error

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		client = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		extractor = v1mock.NewFakeImageExtractor(logger)
		cloudInit = &v1mock.FakeCloudInitRunner{}
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())

		// Config object with all of our fakes on it
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(client),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
			agentConfig.WithPlatform("linux/amd64"),
		)
	})

	AfterEach(func() {
		GinkgoWriter.Println(memLog.String())
		cleanup()
	})

	Describe("Listing extensions", func() {
		It("should NOT fail if the bootstate is not valid", func() {
			extensions, err := action.ListSystemExtensions(config, "invalid")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		Describe("With no dir", func() {
			It("should return no extensions for installed extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for active enabled extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for passive enabled extensions", func() {
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "passive")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("With empty dir", func() {
			It("should return no extensions", func() {
				err := vfs.MkdirAll(config.Fs, "/var/lib/kairos/extensions", 0755)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for active enabled extensions", func() {
				err := vfs.MkdirAll(config.Fs, "/var/lib/kairos/extensions", 0755)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for passive enabled extensions", func() {
				err := vfs.MkdirAll(config.Fs, "/var/lib/kairos/extensions", 0755)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "passive")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("With dir with files", func() {
			BeforeEach(func() {
				err := vfs.MkdirAll(config.Fs, "/var/lib/kairos/extensions", 0755)
				Expect(err).ToNot(HaveOccurred())
			})
			AfterEach(func() { cleanup() })
			It("should not return files that are not valid extensions", func() {
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/invalid", []byte("invalid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return files that are valid extensions", func() {
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(Equal([]action.SysExtension{
					{
						Name:     "valid.raw",
						Location: "/var/lib/kairos/extensions/valid.raw",
					},
				}))
			})
			It("should ONLY return files that are valid extensions", func() {
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/invalid", []byte("invalid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(len(extensions)).To(Equal(1))
				Expect(extensions).To(Equal([]action.SysExtension{
					{
						Name:     "valid.raw",
						Location: "/var/lib/kairos/extensions/valid.raw",
					},
				}))
			})
		})
	})
	Describe("Enabling extensions", func() {
		It("should fail if bootState is not valid", func() { Skip("not implemented") })
		It("should enable an installed extension", func() { Skip("not implemented") })
		It("should fail to enable a missing extension", func() { Skip("not implemented") })
		It("should not fail if the extension is already enabled", func() { Skip("not implemented") })

	})
	Describe("Disabling extensions", func() {
		It("should fail if bootState is not valid", func() { Skip("not implemented") })
		It("should disable an enabled extension", func() { Skip("not implemented") })
		It("should fail to disable a missing extension", func() { Skip("not implemented") })
		It("should not fail if the extension is already disabled", func() { Skip("not implemented") })
	})
	Describe("Installing extensions", func() {
		Describe("With a file source", func() {
			It("should install a extension", func() { Skip("not implemented") })
			It("should fail to install a missing extension", func() { Skip("not implemented") })
		})
		Describe("with a docker source", func() {
			It("should install a extension", func() { Skip("not implemented") })
			It("should fail to install a missing extension", func() { Skip("not implemented") })
		})
		Describe("with a http source", func() {
			It("should install a extension", func() { Skip("not implemented") })
			It("should fail to install a missing extension", func() { Skip("not implemented") })
		})
	})
	Describe("Removing extensions", func() {
		It("should remove an installed extension", func() { Skip("not implemented") })
		It("should disable and remove an enabled extension", func() { Skip("not implemented") })
		It("should fail to remove a missing extension", func() { Skip("not implemented") })
		It("should not fail if the extension is already removed", func() { Skip("not implemented") })
	})
})
