package agent

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("upgrade helpers", func() {
	Describe("generateUpgradeConfForCLIArgs", func() {
		It("serializes all fields to JSON", func() {
			// with source + entry + insecure + excludes → all fields serialized.
			out, err := generateUpgradeConfForCLIArgs("oci:foo:tag", "recovery-entry", true, "/x", "/y")
			Expect(err).ToNot(HaveOccurred())
			for _, s := range []string{
				`"entry":"recovery-entry"`,
				`"allow-insecure-registries":true`,
				`"source":"oci:foo:tag"`,
				`"excluded-paths":["/x","/y"]`,
			} {
				Expect(out).To(ContainSubstring(s), "missing %q in %q", s, out)
			}
		})

		It("omits empty fields on the minimal form", func() {
			out, err := generateUpgradeConfForCLIArgs("", "", false)
			Expect(err).ToNot(HaveOccurred())
			// entry/source/excluded-paths/allow-insecure-registries are all omitempty
			// on this smallest form — resulting JSON is basically just the upgrade
			// wrapper omitting empty children.
			Expect(out).ToNot(ContainSubstring("source"), "unexpected verbose output: %q", out)
			Expect(out).ToNot(ContainSubstring("excluded-paths"), "unexpected verbose output: %q", out)
		})
	})

	// CurrentImage/ListAllReleases/ListNewerReleases all call
	// versioneer.NewArtifactFromOSRelease which reads /etc/kairos-release from the
	// real filesystem; in a test env this file is typically absent so the
	// functions return an error. We just want to exercise the error paths.
	Describe("release listing without os-release", func() {
		It("CurrentImage errors", func() {
			_, err := CurrentImage("registry.example/foo")
			if err == nil {
				Skip("os-release present, cannot exercise error path")
			}
		})

		It("ListAllReleases errors", func() {
			_, err := ListAllReleases(false, "registry.example/foo")
			if err == nil {
				Skip("os-release present, cannot exercise error path")
			}
		})

		It("ListNewerReleases errors", func() {
			_, err := ListNewerReleases(true, "registry.example/foo")
			if err == nil {
				Skip("os-release present, cannot exercise error path")
			}
		})
	})
})
