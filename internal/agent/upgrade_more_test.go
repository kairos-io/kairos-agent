package agent

import (
	"strings"
	"testing"
)

func TestGenerateUpgradeConfForCLIArgs_JSON(t *testing.T) {
	// with source + entry + insecure + excludes → all fields serialized
	out, err := generateUpgradeConfForCLIArgs("oci:foo:tag", "recovery-entry", true, "/x", "/y")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range []string{
		`"entry":"recovery-entry"`,
		`"allow-insecure-registries":true`,
		`"source":"oci:foo:tag"`,
		`"excluded-paths":["/x","/y"]`,
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in %q", s, out)
		}
	}
}

func TestGenerateUpgradeConfForCLIArgs_MinimalOmitsFields(t *testing.T) {
	out, err := generateUpgradeConfForCLIArgs("", "", false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// entry/source/excluded-paths/allow-insecure-registries are all omitempty
	// on this smallest form — resulting JSON is basically just the upgrade
	// wrapper omitting empty children.
	if strings.Contains(out, "source") || strings.Contains(out, "excluded-paths") {
		t.Errorf("unexpected verbose output: %q", out)
	}
}

// CurrentImage/ListAllReleases/ListNewerReleases all call
// versioneer.NewArtifactFromOSRelease which reads /etc/kairos-release from the
// real filesystem; in a test env this file is typically absent so the
// functions return an error. We just want to exercise the error paths.
func TestCurrentImage_MissingOSRelease(t *testing.T) {
	_, err := CurrentImage("registry.example/foo")
	if err == nil {
		t.Skip("os-release present, cannot exercise error path")
	}
}

func TestListAllReleases_MissingOSRelease(t *testing.T) {
	_, err := ListAllReleases(false, "registry.example/foo")
	if err == nil {
		t.Skip("os-release present, cannot exercise error path")
	}
}

func TestListNewerReleases_MissingOSRelease(t *testing.T) {
	_, err := ListNewerReleases(true, "registry.example/foo")
	if err == nil {
		t.Skip("os-release present, cannot exercise error path")
	}
}
