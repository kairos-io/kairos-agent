package action_test

import (
	"bytes"
	"fmt"
	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("Sysext Actions test", func() {
	var config *agentConfig.Config
	var runner *v1mock.FakeRunner
	var fs vfs.FS
	var logger sdkTypes.KairosLogger
	var mounter *v1mock.ErrorMounter
	var syscall *v1mock.FakeSyscall
	var httpClient *v1mock.FakeHTTPClient
	var cloudInit *v1mock.FakeCloudInitRunner
	var cleanup func()
	var memLog *bytes.Buffer
	var extractor *v1mock.FakeImageExtractor
	var err error

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscall = &v1mock.FakeSyscall{}
		mounter = v1mock.NewErrorMounter()
		httpClient = &v1mock.FakeHTTPClient{}
		memLog = &bytes.Buffer{}
		logger = sdkTypes.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		extractor = v1mock.NewFakeImageExtractor(logger)
		cloudInit = &v1mock.FakeCloudInitRunner{}
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())

		err := vfs.MkdirAll(fs, "/var/lib/kairos/extensions", 0755)
		Expect(err).ToNot(HaveOccurred())
		err = vfs.MkdirAll(fs, "/run/extensions", 0755)
		Expect(err).ToNot(HaveOccurred())

		// Config object with all of our fakes on it
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(httpClient),
			agentConfig.WithCloudInitRunner(cloudInit),
			agentConfig.WithImageExtractor(extractor),
			agentConfig.WithPlatform("linux/amd64"),
		)
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("Listing extensions", func() {
		It("should NOT fail if the bootstate is not valid", func() {
			extensions, err := action.ListSystemExtensions(config, "invalid")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		Describe("With no dir", func() {
			BeforeEach(func() {
				err = config.Fs.RemoveAll("/var/lib/kairos/extensions")
				Expect(err).ToNot(HaveOccurred())
			})
			AfterEach(func() {
				cleanup()
			})
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
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for active enabled extensions", func() {
				extensions, err := action.ListSystemExtensions(config, "active")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
			It("should return no extensions for passive enabled extensions", func() {
				extensions, err := action.ListSystemExtensions(config, "passive")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("With dir with files", func() {
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
		It("should fail to enable a extension if bootState is not valid", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			err = action.EnableSystemExtension(config, "valid", "invalid", false)
			Expect(err).To(HaveOccurred())
		})
		It("should enable an installed extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Passive should be empty
			extensions, err = action.ListSystemExtensions(config, "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for passive
			err = action.EnableSystemExtension(config, "valid.raw", "passive", false)
			Expect(err).ToNot(HaveOccurred())
			// Passive should have the extension
			extensions, err = action.ListSystemExtensions(config, "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/passive/valid.raw",
				},
			}))
			// Check active again to see if it is still there
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))

		})
		It("should enable an installed extension and reload the system with it", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			// Fake the boot state
			Expect(config.Fs.Mkdir("/run/cos", 0755)).ToNot(HaveOccurred())
			Expect(config.Fs.WriteFile("/run/cos/active_mode", []byte("true"), 0644)).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			// This basically returns an error if the command is not executed
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Passive should be empty
			extensions, err = action.ListSystemExtensions(config, "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Symlink should be created in /run/extensions
			_, err = config.Fs.Stat("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			readlink, err := config.Fs.Readlink("/run/extensions/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			// Get the raw path as the readlink will return the real path, not the one in our fake fs
			realPath, err := config.Fs.RawPath("/var/lib/kairos/extensions/active/valid.raw")
			Expect(err).ToNot(HaveOccurred())
			Expect(readlink).To(Equal(realPath))
		})
		It("should enable an installed extension and NOT reload the system with it if we are on the wrong boot state", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			// This basically returns an error if the command is not executed
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", true)
			Expect(err).ToNot(HaveOccurred())
			Expect(runner.IncludesCmds([][]string{
				{"systemctl", "restart", "systemd-sysext"},
			})).To(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Passive should be empty
			extensions, err = action.ListSystemExtensions(config, "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Symlink should be created in /run/extensions
			_, err = config.Fs.Stat("/run/extensions/valid.raw")
			Expect(err).To(HaveOccurred())
		})
		It("should fail to enable a missing extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "invalid.raw", "active", false)
			Expect(err).To(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Passive should be empty
			extensions, err = action.ListSystemExtensions(config, "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should not fail if the extension is already enabled", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			err = action.EnableSystemExtension(config, "valid.raw", "active", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
		})

	})
	Describe("Disabling extensions", func() {
		It("should fail if bootState is not valid", func() {
			err := action.DisableSystemExtension(config, "whatever", "invalid")
			Expect(err).To(HaveOccurred())
		})
		It("should disable an enabled extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Disable it
			err = action.DisableSystemExtension(config, "valid.raw", "active")
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should not fail to disable a not enabled extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			// Disable a non enabled extension
			err = action.DisableSystemExtension(config, "invalid.raw", "active")
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
		})
	})
	Describe("Installing extensions", func() {
		Describe("With a file source", func() {
			It("should install a extension", func() {
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
				err = config.Fs.WriteFile("/valid.raw", []byte("valid"), 0644)
				Expect(err).ToNot(HaveOccurred())
				err = action.InstallSystemExtension(config, "file:///valid.raw")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				// Check if the extension is installed
				extensions, err = action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(Equal([]action.SysExtension{
					{
						Name:     "valid.raw",
						Location: "/var/lib/kairos/extensions/valid.raw",
					},
				}))
			})
			It("should fail to install a missing extension", func() {
				extensions, err := action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
				err = action.InstallSystemExtension(config, "file:///invalid.raw")
				Expect(err).To(HaveOccurred(), memLog.String())
				// Check if the extension is installed
				extensions, err = action.ListSystemExtensions(config, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(extensions).To(BeEmpty())
			})
		})
		Describe("with a docker source", func() {
			It("should install a extension", func() {
				err = action.InstallSystemExtension(config, "docker://quay.io/valid:v1.0.0")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				expectedCall := v1mock.ExtractCall{ImageRef: "quay.io/valid:v1.0.0", Destination: "/var/lib/kairos/extensions/", PlatformRef: ""}
				Expect(extractor.WasCalledWithExtractCall(expectedCall)).To(BeTrue())
			})
			It("should fail to install a missing extension", func() {
				extractor.SideEffect = func(imageRef, destination, platformRef string) error {
					return fmt.Errorf("error")
				}
				err = action.InstallSystemExtension(config, "docker://quay.io/invalid:v1.0.0")
				Expect(err).To(HaveOccurred(), memLog.String())
				expectedCall := v1mock.ExtractCall{ImageRef: "quay.io/invalid:v1.0.0", Destination: "/var/lib/kairos/extensions/", PlatformRef: ""}
				Expect(extractor.WasCalledWithExtractCall(expectedCall)).To(BeTrue())
			})
		})
		Describe("with a http source", func() {
			It("should install a extension", func() {
				err = action.InstallSystemExtension(config, "http://localhost:8080/valid.raw")
				Expect(err).ToNot(HaveOccurred(), memLog.String())
				Expect(httpClient.WasGetCalledWith("http://localhost:8080/valid.raw")).To(BeTrue())
			})
			It("should fail to install a missing extension", func() {
				httpClient.Error = true
				err = action.InstallSystemExtension(config, "http://localhost:8080/invalid.raw")
				Expect(err).To(HaveOccurred())
				Expect(httpClient.WasGetCalledWith("http://localhost:8080/invalid.raw")).To(BeTrue())
			})
		})
	})
	Describe("Removing extensions", func() {
		It("should remove an installed extension", func() {
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
			err = action.RemoveSystemExtension(config, "valid.raw")
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should disable and remove an enabled extension", func() {
			err = config.Fs.WriteFile("/var/lib/kairos/extensions/valid.raw", []byte("valid"), 0644)
			Expect(err).ToNot(HaveOccurred())
			extensions, err := action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Enable it for active
			err = action.EnableSystemExtension(config, "valid.raw", "active", false)
			Expect(err).ToNot(HaveOccurred())
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(Equal([]action.SysExtension{
				{
					Name:     "valid.raw",
					Location: "/var/lib/kairos/extensions/active/valid.raw",
				},
			}))
			err = action.RemoveSystemExtension(config, "valid.raw")
			Expect(err).ToNot(HaveOccurred())
			// Check if it is removed from active
			extensions, err = action.ListSystemExtensions(config, "active")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Check if it is removed from passive
			extensions, err = action.ListSystemExtensions(config, "passive")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
			// Check if it is removed from installed
			extensions, err = action.ListSystemExtensions(config, "")
			Expect(err).ToNot(HaveOccurred())
			Expect(extensions).To(BeEmpty())
		})
		It("should fail to remove a missing extension", func() {
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
			err = action.RemoveSystemExtension(config, "invalid.raw")
			Expect(err).To(HaveOccurred())
		})
	})
})
