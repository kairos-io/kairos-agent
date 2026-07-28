package phonehome

import (
	"context"
	"os"
)

// Test-only exports. Files ending in `_test.go` are compiled into the test
// binary only, so the helpers below are invisible to production code.

// SetUninstallRunners swaps the teardown's package-local helpers and
// returns a restorer that puts the production defaults back. Use it in a
// defer (or BeforeEach/AfterEach) so specs don't leak fakes into each other.
func SetUninstallRunners(
	run func(name string, args ...string) ([]byte, error),
	rm func(path string) error,
	read func(path string) ([]byte, error),
	write func(path string, data []byte, perm os.FileMode) error,
	glob func(pattern string) ([]string, error),
) func() {
	prevRun, prevRm := runCommand, removeFile
	prevRead, prevWrite, prevGlob := readFile, writeFile, globFiles
	runCommand = run
	removeFile = rm
	readFile = read
	writeFile = write
	globFiles = glob
	return func() {
		runCommand = prevRun
		removeFile = prevRm
		readFile = prevRead
		writeFile = prevWrite
		globFiles = prevGlob
	}
}

// SetOEMSeams redirects the /oem writes performed by writeOEMCloudConfig to a
// test-owned directory and stubs out the mount attempt. Returns a restorer.
func SetOEMSeams(dir string, mount func() error) func() {
	prevDir, prevMount := oemDir, oemMountCommand
	oemDir = dir
	oemMountCommand = mount
	return func() {
		oemDir = prevDir
		oemMountCommand = prevMount
	}
}

// SetKairosAgentUpgrade swaps the exec of "kairos-agent upgrade" for a test
// stub so handleUpgrade can be driven end-to-end without a real installer.
func SetKairosAgentUpgrade(fn func(context.Context, ...string) ([]byte, error)) func() {
	prev := kairosAgentUpgrade
	kairosAgentUpgrade = fn
	return func() { kairosAgentUpgrade = prev }
}

// SetRebootScheduler exposes the reboot-scheduler seam to external tests so
// they can observe (or suppress) the goroutine that would call reboot(8).
func SetRebootScheduler(fn func()) func() {
	prev := rebootScheduler
	rebootScheduler = fn
	return func() { rebootScheduler = prev }
}
