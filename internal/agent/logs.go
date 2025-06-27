package agent

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/types"
)

// LogsResult represents the collected logs
type LogsResult struct {
	Journal map[string][]byte `yaml:"-"`
	Files   map[string][]byte `yaml:"-"`
}

// LogsCollector handles the collection of logs from various sources
type LogsCollector struct {
	config *config.Config
}

// NewLogsCollector creates a new LogsCollector instance
func NewLogsCollector(cfg *config.Config) *LogsCollector {
	return &LogsCollector{
		config: cfg,
	}
}

func defaultLogsConfig() *config.LogsConfig {
	return &config.LogsConfig{
		Journal: []string{
			"kairos-agent",
			"kairos-installer",
			"kairos-webui",
			"cos-setup-boot",
			"cos-setup-fs",
			"cos-setup-network",
			"cos-setup-reconcile",
			"k3s",
			"k3s-agent",
			"k0scontroller",
			"k0sworker",
		},
		Files: []string{
			"/var/log/kairos/*.log",
			"/run/immucore/*.log",
		},
	}
}

// Collect gathers logs based on the configuration stored in the LogsCollector
func (lc *LogsCollector) Collect() (*LogsResult, error) {
	result := &LogsResult{
		Journal: make(map[string][]byte),
		Files:   make(map[string][]byte),
	}

	// Define default configuration
	logsConfig := defaultLogsConfig()

	// Merge user configuration with defaults
	if lc.config.Logs != nil {
		logsConfig.Journal = append(logsConfig.Journal, lc.config.Logs.Journal...)
		logsConfig.Files = append(logsConfig.Files, lc.config.Logs.Files...)
	}

	// Collect journal logs
	for _, service := range logsConfig.Journal {
		output, err := lc.config.Runner.Run("journalctl", "-u", service, "--no-pager", "-o", "cat")
		if err != nil {
			lc.config.Logger.Warnf("Failed to collect journal logs for service %s: %v", service, err)
			continue
		}

		// Skip services with no journal entries
		if len(output) == 0 || string(output) == "-- No entries --" {
			lc.config.Logger.Debugf("No journal entries found for service %s, skipping", service)
			continue
		}

		result.Journal[service] = output
	}

	// Collect file logs with globbing support
	for _, pattern := range logsConfig.Files {
		matches, err := lc.globFiles(pattern)
		if err != nil {
			lc.config.Logger.Warnf("Failed to glob pattern %s: %v", pattern, err)
			continue
		}

		for _, file := range matches {
			content, err := lc.config.Fs.ReadFile(file)
			if err != nil {
				lc.config.Logger.Warnf("Failed to read file %s: %v", file, err)
				continue
			}
			result.Files[file] = content
		}
	}

	return result, nil
}

// CreateTarball creates a compressed tarball from the collected logs
func (lc *LogsCollector) CreateTarball(result *LogsResult, outputPath string) error {
	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := fsutils.MkdirAll(lc.config.Fs, outputDir, constants.DirPerm); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create the tarball file
	file, err := lc.config.Fs.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create tarball file: %w", err)
	}
	defer file.Close()

	// Create gzip writer
	gw := gzip.NewWriter(file)
	defer gw.Close()

	// Create tar writer
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Add journal logs to tarball
	for service, content := range result.Journal {
		header := &tar.Header{
			Name: fmt.Sprintf("journal/%s.log", service),
			Mode: constants.FilePerm,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write journal header: %w", err)
		}
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write journal content: %w", err)
		}
	}

	// Add file logs to tarball
	for filePath, content := range result.Files {
		// Remove leading slash and use full path structure
		relativePath := strings.TrimPrefix(filePath, "/")
		header := &tar.Header{
			Name: fmt.Sprintf("files/%s", relativePath),
			Mode: constants.FilePerm,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write file header: %w", err)
		}
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write file content: %w", err)
		}
	}

	return nil
}

// globFiles expands glob patterns to matching files
func (lc *LogsCollector) globFiles(pattern string) ([]string, error) {
	// Handle wildcard patterns like "*.log", "kairos-*.log", or "/var/log/kairos/*"
	if strings.Contains(pattern, "*") {
		dir := filepath.Dir(pattern)
		base := filepath.Base(pattern)

		// List files in directory
		entries, err := lc.config.Fs.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		var matches []string
		for _, entry := range entries {
			if !entry.IsDir() {
				fileName := entry.Name()
				if lc.matchesPattern(fileName, base) {
					matches = append(matches, filepath.Join(dir, fileName))
				}
			}
		}
		return matches, nil
	}

	// No glob pattern, return the file if it exists
	if exists, _ := fsutils.Exists(lc.config.Fs, pattern); exists {
		return []string{pattern}, nil
	}

	return nil, nil
}

// matchesPattern checks if a filename matches a glob pattern
func (lc *LogsCollector) matchesPattern(fileName, pattern string) bool {
	// Convert glob pattern to regex pattern
	// Escape special regex characters and replace * with .*
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, "\\*", ".*")

	// Compile the regex pattern
	re, err := regexp.Compile("^" + regexPattern + "$")
	if err != nil {
		// If regex compilation fails, fall back to exact match
		return fileName == pattern
	}

	return re.MatchString(fileName)
}

// ExecuteLogsCommand executes the logs command with the given parameters
func ExecuteLogsCommand(fs v1.FS, logger types.KairosLogger, runner v1.Runner, outputPath string) error {
	// Scan for configuration from default locations
	cfg, err := config.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
	if err != nil {
		logger.Warnf("Failed to load configuration, using defaults: %v", err)
		// Create a minimal config with just the required components
		cfg = config.NewConfig(
			config.WithFs(fs),
			config.WithLogger(logger),
			config.WithRunner(runner),
		)
	} else {
		// Update the scanned config with the provided components
		cfg.Fs = fs
		cfg.Logger = logger
		cfg.Runner = runner
	}

	collector := NewLogsCollector(cfg)

	result, err := collector.Collect()
	if err != nil {
		return fmt.Errorf("failed to collect logs: %w", err)
	}

	if err := collector.CreateTarball(result, outputPath); err != nil {
		return fmt.Errorf("failed to create tarball: %w", err)
	}

	logger.Infof("Logs collected successfully to %s", outputPath)
	return nil
}
