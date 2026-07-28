package agent

import (
	"testing"
)

func TestGetConfig_ValidArgsReturnsConfig(t *testing.T) {
	// getConfig funnels the CLI args through generateUpgradeConfForCLIArgs
	// and then config.Scan. The happy path is exercised here to cover the
	// success branch — the caller (upgrade / upgradeUki) is exercised by
	// other tests. Note: we intentionally do not exercise the full upgrade()
	// flow with a nousers cloud-config here because NewUpgradeSpec panics
	// with a nil-deref when it reaches setUpgradeSourceSize in a test env
	// with no state/recovery partitions (upstream bug — see pkg/config
	// spec.go GetSourceSize).
	dir := t.TempDir()
	cfg, err := getConfig("oci:foo:tag", []string{dir}, "some-entry", false, true, "/x")
	if err != nil {
		t.Fatalf("getConfig err: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}
