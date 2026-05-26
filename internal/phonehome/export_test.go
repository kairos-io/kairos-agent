package phonehome

import (
	"context"
	"os"
	"os/exec"
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

// ParseExtensionArgsForTest is the test-only entry point for the package-private
// parseExtensionArgs validator. Keeping the production function unexported lets
// us reshape it without breaking external callers.
func ParseExtensionArgsForTest(in map[string]string) (ExtensionArgs, error) {
	return parseExtensionArgs(in)
}

// SetExecCommand swaps the shell-out indirection used by the extension
// handlers and (once Task 7 lands) by handleUpgrade. Returns a restorer.
//
// Tests typically pass a function that records args and returns
// `exec.Command("/bin/true")` for success or `exec.Command("/bin/false")`
// to simulate a non-zero exit.
func SetExecCommand(fn func(name string, args ...string) *exec.Cmd) func() {
	prev := execCommand
	execCommand = fn
	return func() { execCommand = prev }
}

// HandleExtensionForTest is the test-only entry point for the package-private
// handleExtension dispatcher. (DefaultCommandHandler also reaches it via the
// switch case, but going through DefaultCommandHandler in every spec is noisy.)
func HandleExtensionForTest(cmd CommandData) (string, error) {
	return handleExtension(context.Background(), cmd)
}

// ParseBundledExtensionsForTest is the test-only entry point for the
// package-private parseBundledExtensions helper used by handleUpgrade to
// decode CommandData.Args["extensions"].
func ParseBundledExtensionsForTest(raw string) ([]BundledExtension, error) {
	return parseBundledExtensions(raw)
}

// SetExtensionsPersistentRoot swaps the on-disk root resolver used by the
// scope-scan helpers. Returns a restorer.
func SetExtensionsPersistentRoot(fn func(extType string) string) func() {
	prev := extensionsPersistentRoot
	extensionsPersistentRoot = fn
	return func() { extensionsPersistentRoot = prev }
}

// ExtensionEnabledAnywhereForTest exposes the package-private
// extensionEnabledAnywhere scanner so specs can verify the prefix-then-dot
// matching behaviour against a fake persistent root.
func ExtensionEnabledAnywhereForTest(extType, name string) bool {
	return extensionEnabledAnywhere(extType, name)
}

// InstallBundledExtensionForTest exposes the compound install+enable helper
// so specs can drive it without going through the full handleUpgrade flow.
func InstallBundledExtensionForTest(e BundledExtension, scope string) error {
	return installBundledExtension(context.Background(), e, scope)
}
