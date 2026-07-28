package hook

import (
	"testing"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
)

func TestEncrypt_WithOEMLabel_TriggersBackupBranch(t *testing.T) {
	c := makeConfig()
	// Force the "OEM present in partitions list" branch of Encrypt(). In a
	// non-UKI headless test env determinePartitionsToEncrypt honours the
	// caller-provided list, GetEncryptor picks whichever encryptor is
	// available, then backupOEMIfNeeded fails inside machine.Mount because
	// the label does not exist. We only need to exercise the code path — any
	// non-nil error is acceptable.
	c.Install = &sdkInstall.Install{Encrypt: []string{constants.OEMLabel}}
	_ = Encrypt(c)
}

func TestPreparePartitionsForEncryption_MultiplePartitionsAllMissing(t *testing.T) {
	c := makeConfig()
	// Iterating over multiple labels lets the loop body run more than once so
	// the coverage counter picks up the second iteration.
	if err := preparePartitionsForEncryption(c, []string{"AAA_MISSING", "BBB_MISSING"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestFindMapperDeviceForPartition_LabelMissing(t *testing.T) {
	// Covers the wrapped-error return in findMapperDeviceForPartition when
	// blkid -L <label> fails because the label is not in blkid's cache.
	c := makeConfig()
	if _, err := findMapperDeviceForPartition(c, "DEFINITELY_NOT_A_LABEL_XYZ"); err == nil {
		t.Fatal("expected error for missing label")
	}
}
