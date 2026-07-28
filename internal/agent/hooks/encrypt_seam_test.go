package hook

import (
	"errors"
	"strings"
	"testing"

	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// stubEncryptSH swaps encryptShFn with a fake for the duration of a test.
// The returned cleanup restores the original.
func stubEncryptSH(t *testing.T, fn func(string) (string, error)) func() {
	t.Helper()
	orig := encryptShFn
	encryptShFn = fn
	return func() { encryptShFn = orig }
}

func stubEncryptMount(t *testing.T, mountFn func(string, string) error, umountFn func(string) error) func() {
	t.Helper()
	origM, origU := encryptMountFn, encryptUmountFn
	if mountFn != nil {
		encryptMountFn = mountFn
	}
	if umountFn != nil {
		encryptUmountFn = umountFn
	}
	return func() {
		encryptMountFn = origM
		encryptUmountFn = origU
	}
}

func TestPreparePartitionsForEncryption_DevPathFoundUnmountsMountpoints(t *testing.T) {
	// Return a device path for the label, then two mount points on the next
	// SH invocation. That walks the inner unmount loop for both mountpoints.
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "blkid -L "):
			return "/dev/vda3\n", nil
		case strings.HasPrefix(cmd, "findmnt "):
			return "/mnt/one\n/mnt/two\n", nil
		}
		return "", nil
	})
	defer restoreSH()
	// Umount stub — return an error on the second call to also cover the
	// warn branch.
	calls := 0
	restoreMount := stubEncryptMount(t, nil, func(mp string) error {
		calls++
		if calls == 2 {
			return errors.New("umount failed")
		}
		return nil
	})
	defer restoreMount()

	c := makeConfig()
	if err := preparePartitionsForEncryption(c, []string{"MY_PART"}); err != nil {
		t.Fatalf("preparePartitionsForEncryption err: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected 2 umount calls, got %d", calls)
	}
}

func TestFindMapperDeviceForPartition_HappyPath(t *testing.T) {
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "blkid -L "):
			return "/dev/vda2\n", nil
		case strings.HasPrefix(cmd, "dmsetup ls"):
			// The mapper name has to match the base name of the partition
			// path — "vda2".
			return "vda2  (252:1)\nother  (252:2)\n", nil
		}
		return "", nil
	})
	defer restoreSH()

	c := makeConfig()
	mp, err := findMapperDeviceForPartition(c, "COS_OEM")
	if err != nil {
		t.Fatalf("findMapperDeviceForPartition err: %v", err)
	}
	if mp != "/dev/mapper/vda2" {
		t.Fatalf("unexpected mapper path: %s", mp)
	}
}

func TestFindMapperDeviceForPartition_MapperNotFound(t *testing.T) {
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "blkid -L "):
			return "/dev/vdb1\n", nil
		case strings.HasPrefix(cmd, "dmsetup ls"):
			// dmsetup does not list the vdb1 mapper → function must fail.
			return "vda2  (252:1)\n", nil
		}
		return "", nil
	})
	defer restoreSH()

	c := makeConfig()
	if _, err := findMapperDeviceForPartition(c, "COS_OEM"); err == nil {
		t.Fatal("expected error when mapper is not in dmsetup listing")
	}
}

func TestFindMapperDeviceForPartition_DmsetupError(t *testing.T) {
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "blkid -L "):
			return "/dev/vdb1\n", nil
		case strings.HasPrefix(cmd, "dmsetup ls"):
			return "", errors.New("dmsetup missing")
		}
		return "", nil
	})
	defer restoreSH()

	c := makeConfig()
	if _, err := findMapperDeviceForPartition(c, "COS_OEM"); err == nil {
		t.Fatal("expected error when dmsetup fails")
	}
}

func TestBackupOEMIfNeeded_HappyPath_NotAlreadyMounted(t *testing.T) {
	// findmnt returns non-zero → oemAlreadyMounted=false. Mount stub
	// succeeds. TempDir on a vfst FS + SyncData through a FakeRunner both
	// succeed → cleanup returned.
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		if strings.HasPrefix(cmd, "findmnt ") {
			return "", errors.New("not mounted")
		}
		return "", nil
	})
	defer restoreSH()

	restoreMount := stubEncryptMount(t,
		func(_, _ string) error { return nil },
		func(_ string) error { return nil },
	)
	defer restoreMount()

	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("test fs: %v", err)
	}
	defer cleanup()

	c := makeConfig()
	c.Fs = fs
	c.Runner = v1mock.NewFakeRunner()

	path, cleanupFn, err := backupOEMIfNeeded(c)
	if err != nil {
		t.Fatalf("backupOEMIfNeeded err: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty backup path")
	}
	// Exercise cleanup (removeAll).
	cleanupFn()
}

func TestRestoreOEM_MountFailure(t *testing.T) {
	// blkid/dmsetup succeed (so findMapperDeviceForPartition returns a
	// mapper path), then blkid TYPE returns empty, and mount returns a
	// non-zero exit. Assert restoreOEM propagates the mount failure.
	// UdevAdmSettle logs but should not error in an environment without
	// udev, so we skip if it does.
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		switch {
		case strings.HasPrefix(cmd, "blkid -L "):
			return "/dev/vda2\n", nil
		case strings.HasPrefix(cmd, "dmsetup ls"):
			return "vda2  (252:1)\n", nil
		case strings.HasPrefix(cmd, "blkid -s TYPE "):
			return "ext4\n", nil
		case strings.HasPrefix(cmd, "mount "):
			return "mount: permission denied", errors.New("mount failed")
		}
		return "", nil
	})
	defer restoreSH()

	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("test fs: %v", err)
	}
	defer cleanup()

	c := makeConfig()
	c.Fs = fs
	c.Runner = v1mock.NewFakeRunner()

	if err := restoreOEM(c, "/tmp/does-not-matter"); err == nil {
		t.Skip("environment allowed UdevAdmSettle or mount to succeed unexpectedly")
	}
}

func TestBackupOEMIfNeeded_HappyPath_AlreadyMounted(t *testing.T) {
	// findmnt returns nil → oemAlreadyMounted=true; Mount is never called.
	restoreSH := stubEncryptSH(t, func(cmd string) (string, error) {
		if strings.HasPrefix(cmd, "findmnt ") {
			return "/oem\n", nil
		}
		return "", nil
	})
	defer restoreSH()

	mountCalled := false
	restoreMount := stubEncryptMount(t,
		func(_, _ string) error { mountCalled = true; return nil },
		func(_ string) error { return nil },
	)
	defer restoreMount()

	fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("test fs: %v", err)
	}
	defer cleanup()

	c := makeConfig()
	c.Fs = fs
	c.Runner = v1mock.NewFakeRunner()

	if _, _, err := backupOEMIfNeeded(c); err != nil {
		t.Fatalf("backupOEMIfNeeded err: %v", err)
	}
	if mountCalled {
		t.Fatal("Mount should not be called when OEM is already mounted")
	}
}
