/*
Copyright © 2026 Kairos authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package partitions

import (
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("parseMountEntry", func() {
	DescribeTable("parses /proc/mounts style entries",
		func(line, expectedDev, expectedMount string) {
			dev, mount := parseMountEntry(line)
			Expect(dev).To(Equal(expectedDev))
			Expect(mount).To(Equal(expectedMount))
		},
		// Happy path: a typical root mount entry.
		Entry("simple root entry",
			"/dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0",
			"/dev/sda6", "/"),
		// Happy path: a by-label device path (the case GetMountPointByLabel cares about).
		Entry("by-label device",
			"/dev/disk/by-label/COS_STATE /run/cos/state ext4 rw,relatime 0 0",
			"/dev/disk/by-label/COS_STATE", "/run/cos/state"),
		// Octal-escaped space (\040) in the mountpoint is decoded to a real space.
		Entry("octal escaped space in mountpoint",
			`/dev/sdb1 /mnt/My\040Disk vfat rw 0 0`,
			"/dev/sdb1", "/mnt/My Disk"),
		// Octal-escaped tab (\011) is decoded.
		Entry("octal escaped tab in mountpoint",
			`/dev/sdb1 /mnt/tab\011here vfat rw 0 0`,
			"/dev/sdb1", "/mnt/tab\there"),
		// Octal-escaped newline (\012) is decoded.
		Entry("octal escaped newline in mountpoint",
			`/dev/sdb1 /mnt/new\012line vfat rw 0 0`,
			"/dev/sdb1", "/mnt/new\nline"),
		// Escaped backslash (\\) is decoded to a single backslash.
		Entry("escaped backslash in mountpoint",
			`/dev/sdb1 /mnt/back\\slash vfat rw 0 0`,
			"/dev/sdb1", `/mnt/back\slash`),
		// Multiple escapes combined in a single mountpoint.
		Entry("multiple escapes combined",
			`/dev/sdb1 /mnt/a\040b\011c vfat rw 0 0`,
			"/dev/sdb1", "/mnt/a b\tc"),
		// Extra whitespace between fields is collapsed by strings.Fields.
		Entry("extra whitespace between fields",
			"/dev/sda1    /boot     ext4   rw 0 0",
			"/dev/sda1", "/boot"),
		// Exactly 4 fields is the minimum accepted (dev mount fstype opts).
		Entry("exactly four fields",
			"/dev/sda2 /home xfs rw",
			"/dev/sda2", "/home"),
		// Lines not starting with '/' (e.g. pseudo filesystems) are ignored.
		Entry("non-slash first char (pseudo fs)",
			"proc /proc proc rw,nosuid 0 0",
			"", ""),
		Entry("tmpfs entry ignored",
			"tmpfs /run tmpfs rw,nosuid 0 0",
			"", ""),
		// Comment-like line that does not start with '/' is ignored.
		Entry("comment line ignored",
			"# this is a comment",
			"", ""),
		// Too few fields (only 3) returns empty even though it starts with '/'.
		Entry("too few fields (three)",
			"/dev/sda1 /boot ext4",
			"", ""),
		Entry("too few fields (two)",
			"/dev/sda1 /boot",
			"", ""),
		Entry("too few fields (one)",
			"/onlyonefield",
			"", ""),
	)

	It("panics on an empty line (documents current behaviour of line[0] indexing)", func() {
		// parseMountEntry indexes line[0] before checking length, so an empty
		// string panics. This test documents that contract without changing it.
		Expect(func() { parseMountEntry("") }).To(Panic())
	})
})

var _ = Describe("GetMountPointByLabel", func() {
	It("returns empty string for a label that is not mounted", func() {
		// Reads the real /proc/mounts; a clearly bogus label will never match,
		// so this exercises the not-found path safely.
		Expect(GetMountPointByLabel("THIS_LABEL_DOES_NOT_EXIST_KAIROS_TEST")).To(Equal(""))
	})
})

var _ = Describe("GetPartitionViaDM", func() {
	var fs sdkFS.KairosFS
	var cleanup func()

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		cleanup()
	})

	// NOTE: GetPartitionViaDM builds its sysfs/udev lookup paths via
	// ghw.NewPaths(fs.RawPath("/")). With vfst, RawPath("/") returns the test
	// FS's real temp-dir root, and ghw prefixes /sys/block and /run/udev/data
	// with it. vfst then re-prefixes that already-absolute path with its own
	// root again, so the resulting doubled path never resolves inside the test
	// FS. This makes the "device found" branches unreachable with a vfst-backed
	// FS, so we can only meaningfully assert the not-found path here.
	It("returns nil when sysfs contains no resolvable dm- devices", func() {
		// /sys/block is empty/absent under the doubled prefix, so the loop finds
		// nothing matching the dm- prefix and returns a nil partition.
		Expect(GetPartitionViaDM(fs, "COS_STATE")).To(BeNil())
	})
})
