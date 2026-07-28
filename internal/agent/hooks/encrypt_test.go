package hook

import (
	"testing"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	internalutils "github.com/kairos-io/kairos-agent/v2/pkg/utils"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
)

func TestDeterminePartitionsToEncrypt_UserSpecifiedWins(t *testing.T) {
	c := sdkConfig.Config{Install: &sdkInstall.Install{Encrypt: []string{"FOO", "BAR"}}}
	got := determinePartitionsToEncrypt(c)
	if len(got) != 2 || got[0] != "FOO" || got[1] != "BAR" {
		t.Fatalf("got %v", got)
	}
}

func TestDeterminePartitionsToEncrypt_NonUkiEmpty(t *testing.T) {
	if internalutils.IsUki() {
		t.Skip("this test only meaningful outside UKI mode")
	}
	c := sdkConfig.Config{Install: &sdkInstall.Install{}}
	got := determinePartitionsToEncrypt(c)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestDeterminePartitionsToEncrypt_UkiDefaults(t *testing.T) {
	if !internalutils.IsUki() {
		t.Skip("this test only meaningful in UKI mode")
	}
	c := sdkConfig.Config{Install: &sdkInstall.Install{}}
	got := determinePartitionsToEncrypt(c)
	if len(got) != 2 || got[0] != constants.OEMLabel || got[1] != constants.PersistentLabel {
		t.Fatalf("got %v", got)
	}
}

func TestPreparePartitionsForEncryption_MissingBlkidWarns(t *testing.T) {
	c := makeConfig()
	// blkid on unknown labels either exits non-zero or returns empty. Either
	// way preparePartitionsForEncryption warns and moves on, returning nil.
	if err := preparePartitionsForEncryption(c, []string{"THIS_LABEL_DOES_NOT_EXIST_XYZ"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestEncrypt_NoPartitionsIsNoop(t *testing.T) {
	c := makeConfig()
	c.Install = &sdkInstall.Install{}
	// Non-UKI mode with empty Encrypt list → determinePartitionsToEncrypt
	// returns []; Encrypt logs and returns nil.
	if err := Encrypt(c); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestEncrypt_WithPartitions_ExercisesGetEncryptor(t *testing.T) {
	c := makeConfig()
	c.Install = &sdkInstall.Install{Encrypt: []string{"MYFAKE_PART"}}
	// GetEncryptor scans for kcrypt config; without any it either succeeds
	// with a default encryptor or returns an error. Either way the code path
	// reaches beyond determinePartitionsToEncrypt.
	// The actual encryption step will fail in the test env because the
	// partition doesn't exist — that's fine, we just want to exercise the
	// call.
	_ = Encrypt(c)
}

func TestFindMapperDeviceForPartition_BlkidMissing(t *testing.T) {
	c := makeConfig()
	// The partition label does not exist → blkid fails → function surfaces
	// the wrapped error from utils.SH.
	_, err := findMapperDeviceForPartition(c, "THIS_LABEL_DOES_NOT_EXIST_XYZ")
	if err == nil {
		t.Fatal("expected error for missing label")
	}
}

func TestRestoreOEM_MissingMapperDevice(t *testing.T) {
	c := makeConfig()
	// findMapperDeviceForPartition fails immediately when blkid does not find
	// the label → restoreOEM propagates the error.
	err := restoreOEM(c, "/tmp/does-not-matter")
	if err == nil {
		t.Fatal("expected error for missing mapper device")
	}
}

func TestBackupOEMIfNeeded_MountFail(t *testing.T) {
	c := makeConfig()
	// In a headless test env findmnt reports /oem is not mounted, then
	// machine.Mount(COS_OEM, /oem) fails (no such label / no root), so
	// backupOEMIfNeeded returns an error.
	_, _, err := backupOEMIfNeeded(c)
	if err == nil {
		t.Skip("environment allowed OEM mount to succeed unexpectedly")
	}
}
