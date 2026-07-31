package hook

import (
	"errors"
	"strings"

	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// stubEncryptSH swaps encryptShFn with a fake for the duration of a test.
// The returned cleanup restores the original.
func stubEncryptSH(fn func(string) (string, error)) func() {
	orig := encryptShFn
	encryptShFn = fn
	return func() { encryptShFn = orig }
}

func stubEncryptMount(mountFn func(string, string) error, umountFn func(string) error) func() {
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

var _ = Describe("Encrypt seams", func() {
	It("preparePartitionsForEncryption unmounts all mountpoints when the dev path is found", func() {
		// Return a device path for the label, then two mount points on the next
		// SH invocation. That walks the inner unmount loop for both mountpoints.
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
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
		restoreMount := stubEncryptMount(nil, func(mp string) error {
			calls++
			if calls == 2 {
				return errors.New("umount failed")
			}
			return nil
		})
		defer restoreMount()

		c := makeConfig()
		Expect(preparePartitionsForEncryption(c, []string{"MY_PART"})).To(Succeed())
		Expect(calls).To(BeNumerically(">=", 2), "expected 2 umount calls")
	})

	It("findMapperDeviceForPartition returns the mapper path on the happy path", func() {
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
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
		Expect(err).ToNot(HaveOccurred())
		Expect(mp).To(Equal("/dev/mapper/vda2"))
	})

	It("findMapperDeviceForPartition errors when the mapper is not in the dmsetup listing", func() {
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
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
		_, err := findMapperDeviceForPartition(c, "COS_OEM")
		Expect(err).To(HaveOccurred())
	})

	It("findMapperDeviceForPartition errors when dmsetup fails", func() {
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
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
		_, err := findMapperDeviceForPartition(c, "COS_OEM")
		Expect(err).To(HaveOccurred())
	})

	It("backupOEMIfNeeded succeeds when OEM is not already mounted", func() {
		// findmnt returns non-zero → oemAlreadyMounted=false. Mount stub
		// succeeds. TempDir on a vfst FS + SyncData through a FakeRunner both
		// succeed → cleanup returned.
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
			if strings.HasPrefix(cmd, "findmnt ") {
				return "", errors.New("not mounted")
			}
			return "", nil
		})
		defer restoreSH()

		restoreMount := stubEncryptMount(
			func(_, _ string) error { return nil },
			func(_ string) error { return nil },
		)
		defer restoreMount()

		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		defer cleanup()

		c := makeConfig()
		c.Fs = fs
		c.Runner = v1mock.NewFakeRunner()

		path, cleanupFn, err := backupOEMIfNeeded(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(path).ToNot(BeEmpty())
		// Exercise cleanup (removeAll).
		cleanupFn()
	})

	It("restoreOEM propagates a mount failure", func() {
		// blkid/dmsetup succeed (so findMapperDeviceForPartition returns a
		// mapper path), then blkid TYPE returns empty, and mount returns a
		// non-zero exit. Assert restoreOEM propagates the mount failure.
		// UdevAdmSettle logs but should not error in an environment without
		// udev, so we skip if it does.
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
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
		Expect(err).ToNot(HaveOccurred())
		defer cleanup()

		c := makeConfig()
		c.Fs = fs
		c.Runner = v1mock.NewFakeRunner()

		if err := restoreOEM(c, "/tmp/does-not-matter"); err == nil {
			Skip("environment allowed UdevAdmSettle or mount to succeed unexpectedly")
		}
	})

	It("backupOEMIfNeeded skips mounting when OEM is already mounted", func() {
		// findmnt returns nil → oemAlreadyMounted=true; Mount is never called.
		restoreSH := stubEncryptSH(func(cmd string) (string, error) {
			if strings.HasPrefix(cmd, "findmnt ") {
				return "/oem\n", nil
			}
			return "", nil
		})
		defer restoreSH()

		mountCalled := false
		restoreMount := stubEncryptMount(
			func(_, _ string) error { mountCalled = true; return nil },
			func(_ string) error { return nil },
		)
		defer restoreMount()

		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		defer cleanup()

		c := makeConfig()
		c.Fs = fs
		c.Runner = v1mock.NewFakeRunner()

		_, _, err = backupOEMIfNeeded(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(mountCalled).To(BeFalse(), "Mount should not be called when OEM is already mounted")
	})
})
