package phonehome

// Test-only exports. Files ending in `_test.go` are compiled into the test
// binary only, so the helpers below are invisible to production code.

// SetUninstallRunners swaps the package-local systemctl runner and file
// remover used by Uninstall. Returns the previous values so the caller can
// restore them in a defer. Only tests should touch this.
func SetUninstallRunners(
	run func(name string, args ...string) ([]byte, error),
	rm func(path string) error,
) (func(name string, args ...string) ([]byte, error), func(path string) error) {
	prevRun := runCommand
	prevRm := removeFile
	runCommand = run
	removeFile = rm
	return prevRun, prevRm
}
