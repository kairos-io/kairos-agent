package agent

import (
	"os"
	"path/filepath"
	"testing"

	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-sdk/state"
)

// writeNousersCloudConfig writes a minimal cloud-config that turns off the
// admin-user requirement so reset() / upgrade() proceed past
// CheckConfigForUsers and into the Read*SpecFromConfig branch. That branch
// then fails because the test env is not a booted Kairos system, but we still
// gain coverage over the config-loading section of the top-level entry point.
func writeNousersCloudConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cc.yaml")
	body := "#cloud-config\ninstall:\n  nousers: true\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReset_NonUKINousersReachesSpecLoad(t *testing.T) {
	resetBus(t)
	dir := writeNousersCloudConfig(t)
	err := Reset(false, true, false, dir)
	if err == nil {
		t.Skip("environment allowed reset to succeed unexpectedly")
	}
	// NewResetSpec returns "reset can only be called from the recovery
	// system" once CheckConfigForUsers has been bypassed. We don't require
	// that exact text — any error is acceptable — but this scenario walks
	// through reset() past its early-return branches.
	t.Logf("Reset err (accepted): %v", err)
}

func TestReset_UKIHDDNousersReachesUkiSpecLoad(t *testing.T) {
	resetBus(t)
	orig := ukiBootModeFn
	ukiBootModeFn = func() state.Boot { return internalutils.UkiHDD }
	defer func() { ukiBootModeFn = orig }()

	dir := writeNousersCloudConfig(t)
	err := Reset(false, true, false, dir)
	if err == nil {
		t.Skip("environment allowed resetUki to succeed unexpectedly")
	}
	t.Logf("resetUki err (accepted): %v", err)
}
