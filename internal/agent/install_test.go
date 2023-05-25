package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/kairos-io/kairos/v2/pkg/config"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	v1mock "github.com/kairos-io/kairos/v2/tests/mocks"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
	"gopkg.in/yaml.v3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
	var installConfig v1.RunConfig
	var userCloudConfigFile, resultFile *os.File
	var options map[string]string
	var err error
	var fs vfs.FS
	var cleanup func()
	var cloudInit *v1mock.FakeCloudInitRunner

	BeforeEach(func() {
		userCloudConfigFile, err = os.CreateTemp("", "kairos-config")
		Expect(err).ToNot(HaveOccurred())
		err = userCloudConfigFile.Close()
		Expect(err).ToNot(HaveOccurred())

		resultFile, err = os.CreateTemp("", "kairos-config")
		Expect(err).ToNot(HaveOccurred())
		err = resultFile.Close()
		Expect(err).ToNot(HaveOccurred())

		userConfig := fmt.Sprintf(`
#cloud-config

install:
  image: test
stages:
  before-install:
  - name: "Create a file"
    commands:
      - |
        echo "this was run" > %s
`, resultFile.Name())

		os.WriteFile(userCloudConfigFile.Name(), []byte(userConfig), os.ModePerm)

		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).Should(BeNil())

		cloudInit = &v1mock.FakeCloudInitRunner{}

		installConfig = v1.RunConfig{
			Debug: false,
			Config: v1.Config{
				Logger:          v1.NewNullLogger(),
				Fs:              vfs.NewReadOnlyFS(fs),
				CloudInitRunner: cloudInit,
			},
			CloudInitPaths: []string{userCloudConfigFile.Name()},
		}

		options = map[string]string{
			"cc": userConfig,
		}
	})

	AfterEach(func() {
		cleanup()
		os.RemoveAll(userCloudConfigFile.Name())
		os.RemoveAll(resultFile.Name())
	})

	FIt("respects the user defined after-install stages", func() {
		// TODO:
		// - Refactor RunInstall to accept the installConfig pat (/etc/elemental)
		//   as part of `options` (caller still passes the default "/etc/elemental")
		// - Pass an installConfig here and a cloud-config that defines an after-install stage
		// - Check that the after-install stage code has run (check the result of that stage?)
		err = RunInstall(&installConfig, options)
		Expect(err).ToNot(HaveOccurred())
		out, err := os.ReadFile(resultFile.Name())
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(Equal("this was run"))
	})
})
