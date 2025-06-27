package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = FDescribe("Logs Command", Label("logs", "cmd"), func() {
	var (
		fs      *vfst.TestFS
		cleanup func()
		err     error
		logger  types.KairosLogger
		runner  *v1mock.FakeRunner
	)

	BeforeEach(func() {
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())

		logger = types.NewNullLogger()
		runner = v1mock.NewFakeRunner()
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("LogsCollector", func() {
		var collector *LogsCollector

		BeforeEach(func() {
			cfg := config.NewConfig(
				config.WithFs(fs),
				config.WithLogger(logger),
				config.WithRunner(runner),
			)
			collector = NewLogsCollector(cfg)
		})

		It("should collect journal logs", func() {
			// Mock journalctl command output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 0 && args[0] == "-u" {
					service := args[1]
					return []byte("journal logs for " + service), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"myservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify journalctl was called
			Expect(runner.CmdsMatch([][]string{{"journalctl", "-u", "myservice", "--no-pager", "-o", "cat"}})).To(BeNil())
		})

		It("should collect file logs with globbing", func() {
			// Create test files
			testFiles := []string{
				"/var/log/test1.log",
				"/var/log/test2.log",
				"/var/log/subdir/test3.log",
			}

			for _, file := range testFiles {
				err := fsutils.MkdirAll(fs, filepath.Dir(file), constants.DirPerm)
				Expect(err).ToNot(HaveOccurred())

				err = fs.WriteFile(file, []byte("log content for "+file), constants.FilePerm)
				Expect(err).ToNot(HaveOccurred())
			}

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/*.log", "/var/log/subdir/*.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify files were collected
			Expect(result.Files).To(HaveLen(3))
		})

		It("should handle nested directories correctly", func() {
			// Create test files with same basename in different directories
			testFiles := []string{
				"/var/log/test.log",
				"/var/log/subdir/test.log", // Same basename, different directory
			}

			for _, file := range testFiles {
				err := fsutils.MkdirAll(fs, filepath.Dir(file), constants.DirPerm)
				Expect(err).ToNot(HaveOccurred())

				err = fs.WriteFile(file, []byte("log content for "+file), constants.FilePerm)
				Expect(err).ToNot(HaveOccurred())
			}

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/test.log", "/var/log/subdir/test.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify both files are collected with full paths as keys
			Expect(result.Files).To(HaveKey("/var/log/test.log"))
			Expect(result.Files).To(HaveKey("/var/log/subdir/test.log"))
			Expect(result.Files).To(HaveLen(2))

			// Create tarball
			tarballPath := "/tmp/logs.tar.gz"
			err = collector.CreateTarball(result, tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Read and verify tarball contents
			tarballData, err := fs.ReadFile(tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Extract and verify tarball structure
			gzr, err := gzip.NewReader(bytes.NewReader(tarballData))
			Expect(err).ToNot(HaveOccurred())
			defer gzr.Close()

			tr := tar.NewReader(gzr)
			files := make(map[string]bool)

			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				files[header.Name] = true
			}

			// Verify that both files are preserved with their full directory structure
			Expect(files).To(HaveKey("files/var/log/test.log"))
			Expect(files).To(HaveKey("files/var/log/subdir/test.log"))
			Expect(files).To(HaveLen(2))
		})

		It("should create tarball with proper structure", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Create test file
			testFile := "/var/log/test.log"
			err := fsutils.MkdirAll(fs, filepath.Dir(testFile), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())

			err = fs.WriteFile(testFile, []byte("file log content"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"myservice"},
				Files:   []string{testFile},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())

			// Create tarball
			tarballPath := "/tmp/logs.tar.gz"
			err = collector.CreateTarball(result, tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Verify tarball exists and has correct structure
			exists, err := fsutils.Exists(fs, tarballPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())

			// Read and verify tarball contents
			tarballData, err := fs.ReadFile(tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Extract and verify tarball structure
			gzr, err := gzip.NewReader(bytes.NewReader(tarballData))
			Expect(err).ToNot(HaveOccurred())
			defer gzr.Close()

			tr := tar.NewReader(gzr)
			files := make(map[string]bool)

			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				files[header.Name] = true
			}

			// Verify expected structure
			Expect(files).To(HaveKey("journal/myservice.log"))
			Expect(files).To(HaveKey("files/var/log/test.log"))
		})

		It("should handle missing files gracefully", func() {
			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/nonexistent.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Files).To(HaveLen(0))
		})

		It("should handle journal service errors gracefully", func() {
			// Mock journalctl to return error
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return nil, fmt.Errorf("service not found")
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"nonexistentservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Journal).To(HaveLen(0))
		})

		It("should use default log sources when no config provided", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("default journal logs"), nil
				}
				return []byte{}, nil
			}

			// Don't set logs config - should use defaults
			collector.config.Logs = nil

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Should have collected from default services
			Expect(runner.CmdsMatch([][]string{
				{"journalctl", "-u", "kairos-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-installer", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s", "--no-pager", "-o", "cat"},
			})).To(BeNil())
		})
	})

	Describe("CLI Command", func() {
		It("should execute logs command successfully", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Create test config file
			configContent := `
logs:
  journal:
  - myservice
  files:
  - /var/log/test.log
`
			configPath := "/etc/kairos/config.yaml"
			err := fsutils.MkdirAll(fs, filepath.Dir(configPath), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())

			err = fs.WriteFile(configPath, []byte(configContent), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Create test log file
			testFile := "/var/log/test.log"
			err = fsutils.MkdirAll(fs, filepath.Dir(testFile), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())

			// Execute logs command
			outputPath := "/tmp/logs.tar.gz"
			err = ExecuteLogsCommand(fs, logger, runner, outputPath)
			Expect(err).ToNot(HaveOccurred())

			// Verify output file was created
			exists, err := fsutils.Exists(fs, outputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("should handle missing config gracefully", func() {
			// Execute logs command without config
			outputPath := "/tmp/logs.tar.gz"
			err := ExecuteLogsCommand(fs, logger, runner, outputPath)
			Expect(err).ToNot(HaveOccurred())

			// Should still create output with default sources
			exists, err := fsutils.Exists(fs, outputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
	})
})
