package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeExecutable creates an executable file at dir/name and returns its path.
func writeExecutable(dir, name string) string {
	p := filepath.Join(dir, name)
	Expect(os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())
	return p
}

// writeFileAt creates an executable file at an absolute path.
func writeFileAt(path string) {
	Expect(os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())
}

var _ = Describe("resolveInstaller", func() {
	var tmp string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
		origOverride, origDefault := installerOverridePath, installerDefaultPath
		installerOverridePath = filepath.Join(tmp, "override")
		installerDefaultPath = filepath.Join(tmp, "default")
		DeferCleanup(func() {
			installerOverridePath, installerDefaultPath = origOverride, origDefault
		})
		GinkgoT().Setenv(installerEnvVar, "")
	})

	It("prefers KAIROS_INSTALLER when the file exists", func() {
		bin := writeExecutable(tmp, "custom")
		GinkgoT().Setenv(installerEnvVar, bin)
		writeFileAt(installerDefaultPath)
		Expect(resolveInstaller()).To(Equal(bin))
	})

	It("falls through when KAIROS_INSTALLER points to a missing file", func() {
		GinkgoT().Setenv(installerEnvVar, filepath.Join(tmp, "nope"))
		Expect(resolveInstaller()).To(BeEmpty())
	})

	It("uses the override path over the default when both exist", func() {
		writeFileAt(installerOverridePath)
		writeFileAt(installerDefaultPath)
		Expect(resolveInstaller()).To(Equal(installerOverridePath))
	})

	It("uses the default path when only it exists", func() {
		writeFileAt(installerDefaultPath)
		Expect(resolveInstaller()).To(Equal(installerDefaultPath))
	})

	It("returns empty when nothing exists", func() {
		Expect(resolveInstaller()).To(BeEmpty())
	})
})

var _ = Describe("installer dispatch", func() {
	Describe("installerCommand", func() {
		It("forwards the source as --source", func() {
			cmd := installerCommand("/bin/installer", "oci://foo:bar")
			Expect(cmd.Args).To(Equal([]string{"/bin/installer", "--source", "oci://foo:bar"}))
		})

		It("omits --source when no source is given", func() {
			cmd := installerCommand("/bin/installer", "")
			Expect(cmd.Args).To(Equal([]string{"/bin/installer"}))
		})
	})

	Describe("runExternalInstaller", func() {
		It("propagates the installer's exit code", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "failing-installer")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\nexit 7\n"), 0o755)).To(Succeed())

			err := runExternalInstaller(bin, "")
			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(7))
		})
	})
})
