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

var _ = Describe("Logs Command", Label("logs", "cmd"), func() {
	var (
		fs      *vfst.TestFS
		cleanup func()
		err     error
		logger  types.KairosLogger
		runner  *v1mock.FakeRunner
	)

	// Helper function to create a mock systemctl file for systemd detection
	createMockSystemctl := func() {
		err := fsutils.MkdirAll(fs, "/usr/bin", constants.DirPerm)
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile("/usr/bin/systemctl", []byte("#!/bin/sh\necho 'mock systemctl'"), constants.FilePerm)
		Expect(err).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())

		logger = types.NewNullLogger()
		runner = v1mock.NewFakeRunner()

		// Create mock systemctl for systemd detection in all tests
		createMockSystemctl()
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
				// Mock diagnostic commands
				return []byte("diagnostic output"), nil
			}

			// Create /etc/resolv.conf for diagnostics
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"myservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify that both default and user-defined services are in the result
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).To(HaveKey("myservice"))
			Expect(result.Journal).To(HaveLen(12))

			// Verify diagnostics were collected
			Expect(result.Diagnostics).To(HaveKey("hostname"))
			Expect(result.Diagnostics).To(HaveKey("uname"))
			Expect(result.Diagnostics).To(HaveKey("resolv.conf"))
		})

		It("should collect file logs with globbing", func() {
			// Create test files
			testFiles := []string{
				"/var/log/test1.log",
				"/var/log/test2.log",
				"/var/log/subdir/test3.log",
				"/var/log/kairos/agent.log", // Default pattern file
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

			// Verify files were collected (both default and user-defined patterns)
			// Default pattern: /var/log/kairos/* (matches agent.log)
			// User patterns: /var/log/*.log, /var/log/subdir/*.log
			Expect(result.Files).To(HaveLen(4))
			Expect(result.Files).To(HaveKey("/var/log/test1.log"))
			Expect(result.Files).To(HaveKey("/var/log/test2.log"))
			Expect(result.Files).To(HaveKey("/var/log/subdir/test3.log"))
			Expect(result.Files).To(HaveKey("/var/log/kairos/agent.log"))
		})

		It("should handle nested directories correctly", func() {
			// Create test files with same basename in different directories
			testFiles := []string{
				"/var/log/test.log",
				"/var/log/subdir/test.log",  // Same basename, different directory
				"/var/log/kairos/agent.log", // Default pattern file
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

			// Verify both files are collected with full paths as keys (including default pattern)
			Expect(result.Files).To(HaveKey("/var/log/test.log"))
			Expect(result.Files).To(HaveKey("/var/log/subdir/test.log"))
			Expect(result.Files).To(HaveKey("/var/log/kairos/agent.log"))
			Expect(result.Files).To(HaveLen(3))

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

			// Verify that all files are preserved with their full directory structure
			Expect(files).To(HaveKey("files/var/log/test.log"))
			Expect(files).To(HaveKey("files/var/log/subdir/test.log"))
			Expect(files).To(HaveKey("files/var/log/kairos/agent.log"))
			// Also verify diagnostics are included
			Expect(files).To(HaveKey("diagnostics/hostname.txt"))
			Expect(files).To(HaveKey("diagnostics/uname.txt"))
			Expect(files).To(HaveKey("diagnostics/resolv.conf.txt"))
		})

		It("should create tarball with proper structure", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				// Mock diagnostic commands
				return []byte("diagnostic output"), nil
			}

			// Create /etc/resolv.conf for diagnostics
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Create test file
			testFile := "/var/log/test.log"
			err = fsutils.MkdirAll(fs, filepath.Dir(testFile), constants.DirPerm)
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

			// Verify expected structure (both default and user-defined)
			// Default services: kairos-agent, kairos-installer, kairos-webui, cos-setup-boot, cos-setup-fs, cos-setup-network, cos-setup-reconcile, k3s, k3s-agent, k0scontroller, k0sworker
			// User service: myservice
			// Default files: /var/log/kairos-*.log (if exists)
			// User file: /var/log/test.log
			// Diagnostics: hostname, uname, lsblk, mount, ip-addr, ps, resolv.conf
			Expect(files).To(HaveKey("journal/kairos-agent.log"))
			Expect(files).To(HaveKey("journal/kairos-installer.log"))
			Expect(files).To(HaveKey("journal/kairos-webui.log"))
			Expect(files).To(HaveKey("journal/cos-setup-boot.log"))
			Expect(files).To(HaveKey("journal/cos-setup-fs.log"))
			Expect(files).To(HaveKey("journal/cos-setup-network.log"))
			Expect(files).To(HaveKey("journal/cos-setup-reconcile.log"))
			Expect(files).To(HaveKey("journal/k3s.log"))
			Expect(files).To(HaveKey("journal/k3s-agent.log"))
			Expect(files).To(HaveKey("journal/k0scontroller.log"))
			Expect(files).To(HaveKey("journal/k0sworker.log"))
			Expect(files).To(HaveKey("journal/myservice.log"))
			Expect(files).To(HaveKey("files/var/log/test.log"))
			Expect(files).To(HaveKey("diagnostics/hostname.txt"))
			Expect(files).To(HaveKey("diagnostics/uname.txt"))
			Expect(files).To(HaveKey("diagnostics/resolv.conf.txt"))
		})

		It("should handle missing files gracefully", func() {
			// Create a default pattern file to ensure it's collected
			defaultFile := "/var/log/kairos/agent.log"
			err := fsutils.MkdirAll(fs, filepath.Dir(defaultFile), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile(defaultFile, []byte("default log content"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/nonexistent.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected the default pattern file even though user file doesn't exist
			Expect(result.Files).To(HaveLen(1))
			Expect(result.Files).To(HaveKey("/var/log/kairos/agent.log"))
		})

		It("should handle journal service errors gracefully", func() {
			// Mock journalctl to return no entries for non-existent service
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 1 && args[1] == "nonexistentservice" {
					return []byte("-- No entries --"), nil
				}
				if command == "journalctl" {
					return []byte("journal logs content"), nil
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
			// Should have collected from default services but not the non-existent user service
			Expect(result.Journal).To(HaveLen(11))
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).ToNot(HaveKey("nonexistentservice"))
		})

		It("should handle empty journal output gracefully", func() {
			// Mock journalctl to return empty output for user service, normal for defaults
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 1 && args[1] == "emptyservice" {
					return []byte{}, nil
				}
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"emptyservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected from default services but not the empty user service
			Expect(result.Journal).To(HaveLen(11))
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).ToNot(HaveKey("emptyservice"))
		})

		It("should not create tarball files for services with no journal entries", func() {
			// Mock journalctl to return no entries for one service and content for another
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 1 && args[1] == "existingservice" {
					return []byte("journal logs content"), nil
				}
				if command == "journalctl" && len(args) > 1 && args[1] == "nonexistentservice" {
					return []byte("-- No entries --"), nil
				}
				if command == "journalctl" {
					return []byte("default journal logs"), nil
				}
				// Mock diagnostic commands
				return []byte("diagnostic output"), nil
			}

			// Create /etc/resolv.conf for diagnostics
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"existingservice", "nonexistentservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected from default services and the existing user service
			Expect(result.Journal).To(HaveLen(12))
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).To(HaveKey("existingservice"))
			Expect(result.Journal).ToNot(HaveKey("nonexistentservice"))

			// Create tarball and verify contents
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

			// Verify that default services and existing user service files are created
			Expect(files).To(HaveKey("journal/kairos-agent.log"))
			Expect(files).To(HaveKey("journal/kairos-installer.log"))
			Expect(files).To(HaveKey("journal/kairos-webui.log"))
			Expect(files).To(HaveKey("journal/cos-setup-boot.log"))
			Expect(files).To(HaveKey("journal/cos-setup-fs.log"))
			Expect(files).To(HaveKey("journal/cos-setup-network.log"))
			Expect(files).To(HaveKey("journal/cos-setup-reconcile.log"))
			Expect(files).To(HaveKey("journal/k3s.log"))
			Expect(files).To(HaveKey("journal/k3s-agent.log"))
			Expect(files).To(HaveKey("journal/k0scontroller.log"))
			Expect(files).To(HaveKey("journal/k0sworker.log"))
			Expect(files).To(HaveKey("journal/existingservice.log"))
			Expect(files).ToNot(HaveKey("journal/nonexistentservice.log"))
			// Also verify diagnostics are included
			Expect(files).To(HaveKey("diagnostics/hostname.txt"))
			Expect(files).To(HaveKey("diagnostics/resolv.conf.txt"))
		})

		It("should use default log sources when no config provided", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("default journal logs"), nil
				}
				// Mock diagnostic commands
				return []byte("diagnostic output"), nil
			}

			// Create /etc/resolv.conf for diagnostics
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Don't set logs config - should use defaults
			collector.config.Logs = nil

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify default services were collected
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			// Verify diagnostics were collected
			Expect(result.Diagnostics).To(HaveKey("hostname"))
			Expect(result.Diagnostics).To(HaveKey("resolv.conf"))
		})

		It("should merge user logs config with defaults", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 0 && args[0] == "-u" {
					service := args[1]
					return []byte("journal logs for " + service), nil
				}
				// Mock diagnostic commands
				return []byte("diagnostic output"), nil
			}

			// Create /etc/resolv.conf for diagnostics
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config with user-defined services
			collector.config.Logs = &config.LogsConfig{
				Journal: []string{"myservice", "myotherservice"},
				Files:   []string{"/var/log/mycustom.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify that both default and user-defined files are in the result
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).To(HaveKey("myservice"))
			Expect(result.Journal).To(HaveKey("myotherservice"))
			Expect(result.Journal).To(HaveLen(13))
			// Verify diagnostics were collected
			Expect(result.Diagnostics).To(HaveKey("hostname"))
			Expect(result.Diagnostics).To(HaveKey("resolv.conf"))
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

	Describe("Diagnostics Collection", func() {
		var collector *LogsCollector

		BeforeEach(func() {
			cfg := config.NewConfig(
				config.WithFs(fs),
				config.WithLogger(logger),
				config.WithRunner(runner),
			)
			collector = NewLogsCollector(cfg)
		})

		It("should collect diagnostic information", func() {
			// Mock diagnostic commands
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				switch command {
				case "hostname":
					return []byte("test-hostname"), nil
				case "uname":
					return []byte("Linux test-hostname 5.15.0-105-generic"), nil
				case "lsblk":
					return []byte("NAME MAJ:MIN RM SIZE RO TYPE MOUNTPOINT\nsda 8:0 0 50G 0 disk"), nil
				case "mount":
					return []byte("/dev/sda1 on / type ext4 (rw,relatime)"), nil
				case "ip":
					return []byte("1: lo: <LOOPBACK,UP,LOWER_UP>"), nil
				case "ps":
					return []byte("USER PID %CPU %MEM COMMAND"), nil
				default:
					return []byte{}, nil
				}
			}

			// Create /etc/resolv.conf
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify diagnostics were collected
			Expect(result.Diagnostics).To(HaveKey("hostname"))
			Expect(result.Diagnostics).To(HaveKey("uname"))
			Expect(result.Diagnostics).To(HaveKey("lsblk"))
			Expect(result.Diagnostics).To(HaveKey("mount"))
			Expect(result.Diagnostics).To(HaveKey("ip-addr"))
			Expect(result.Diagnostics).To(HaveKey("ps"))
			Expect(result.Diagnostics).To(HaveKey("resolv.conf"))

			// Verify content
			Expect(string(result.Diagnostics["hostname"])).To(Equal("test-hostname"))
			Expect(string(result.Diagnostics["resolv.conf"])).To(Equal("nameserver 8.8.8.8"))
		})

		It("should include diagnostics in tarball", func() {
			// Mock commands
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "hostname" {
					return []byte("test-hostname"), nil
				}
				if command == "journalctl" {
					return []byte("journal logs"), nil
				}
				return []byte("command output"), nil
			}

			// Create /etc/resolv.conf
			err := fsutils.MkdirAll(fs, "/etc", constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile("/etc/resolv.conf", []byte("nameserver 8.8.8.8"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())

			// Create tarball
			tarballPath := "/tmp/diagnostics.tar.gz"
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

			// Verify diagnostics are in tarball
			Expect(files).To(HaveKey("diagnostics/hostname.txt"))
			Expect(files).To(HaveKey("diagnostics/uname.txt"))
			Expect(files).To(HaveKey("diagnostics/lsblk.txt"))
			Expect(files).To(HaveKey("diagnostics/mount.txt"))
			Expect(files).To(HaveKey("diagnostics/ip-addr.txt"))
			Expect(files).To(HaveKey("diagnostics/ps.txt"))
			Expect(files).To(HaveKey("diagnostics/resolv.conf.txt"))
		})

		It("should handle diagnostic command failures gracefully", func() {
			// Mock commands to fail
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "hostname" {
					return nil, fmt.Errorf("command not found")
				}
				if command == "journalctl" {
					return []byte("journal logs"), nil
				}
				return []byte("command output"), nil
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify error message is stored for failed command
			Expect(result.Diagnostics).To(HaveKey("hostname"))
			Expect(string(result.Diagnostics["hostname"])).To(ContainSubstring("Error running command"))
		})

		It("should handle missing resolv.conf gracefully", func() {
			// Mock commands
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("journal logs"), nil
				}
				return []byte("command output"), nil
			}

			// Don't create /etc/resolv.conf
			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify error message is stored for missing file
			Expect(result.Diagnostics).To(HaveKey("resolv.conf"))
			Expect(string(result.Diagnostics["resolv.conf"])).To(ContainSubstring("Error reading file"))
		})
	})
})
