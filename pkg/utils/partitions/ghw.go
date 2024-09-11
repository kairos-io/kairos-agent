package partitions

import (
	"bufio"
	"fmt"
	"github.com/jaypipes/ghw/pkg/util"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	sectorSize = 512
)

type Disk struct {
	Name       string           `json:"name"`
	SizeBytes  uint64           `json:"size_bytes"`
	Partitions v1.PartitionList `json:"partitions"`
}

type Paths struct {
	SysBlock    string
	RunUdevData string
	ProcMounts  string
}

func NewPaths(withOptionalPrefix string) *Paths {
	p := &Paths{
		SysBlock:    "/sys/block/",
		RunUdevData: "/run/udev/data",
		ProcMounts:  "/proc/mounts",
	}
	if withOptionalPrefix != "" {
		withOptionalPrefix = strings.TrimSuffix(withOptionalPrefix, "/")
		p.SysBlock = fmt.Sprintf("%s/%s", withOptionalPrefix, p.SysBlock)
		p.RunUdevData = fmt.Sprintf("%s/%s", withOptionalPrefix, p.RunUdevData)
		p.ProcMounts = fmt.Sprintf("%s/%s", withOptionalPrefix, p.ProcMounts)
	}
	return p
}

func GetDisks(paths *Paths) []*Disk {
	disks := make([]*Disk, 0)
	files, err := os.ReadDir(paths.SysBlock)
	if err != nil {
		return nil
	}
	for _, file := range files {
		dname := file.Name()
		size := diskSizeBytes(paths, dname)

		if strings.HasPrefix(dname, "loop") && size == 0 {
			// We don't care about unused loop devices...
			continue
		}
		d := &Disk{
			Name:      dname,
			SizeBytes: size,
		}

		parts := diskPartitions(paths, dname)
		d.Partitions = parts

		disks = append(disks, d)
	}

	return disks
}

func diskSizeBytes(paths *Paths, disk string) uint64 {
	// We can find the number of 512-byte sectors by examining the contents of
	// /sys/block/$DEVICE/size and calculate the physical bytes accordingly.
	path := filepath.Join(paths.SysBlock, disk, "size")
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		return 0
	}
	return size * sectorSize
}

// diskPartitions takes the name of a disk (note: *not* the path of the disk,
// but just the name. In other words, "sda", not "/dev/sda" and "nvme0n1" not
// "/dev/nvme0n1") and returns a slice of pointers to Partition structs
// representing the partitions in that disk
func diskPartitions(paths *Paths, disk string) v1.PartitionList {
	out := make(v1.PartitionList, 0)
	path := filepath.Join(paths.SysBlock, disk)
	files, err := os.ReadDir(path)
	if err != nil {
		fmt.Println("failed to read disk partitions: %s\n", err)
		return out
	}
	for _, file := range files {
		fname := file.Name()
		if !strings.HasPrefix(fname, disk) {
			continue
		}
		size := partitionSizeBytes(paths, disk, fname)
		mp, pt := partitionInfo(paths, fname)
		du := diskPartUUID(paths, disk, fname)
		if pt == "" {
			pt = diskPartTypeUdev(paths, disk, fname)
		}
		fsLabel := diskFSLabel(paths, disk, fname)
		p := &v1.Partition{
			Name:            fname,
			Size:            uint(size / (1024 * 1024)),
			MountPoint:      mp,
			UUID:            du,
			FilesystemLabel: fsLabel,
			Type:            pt,
			Path:            filepath.Join("/dev", fname),
			Disk:            filepath.Join("/dev", disk),
		}
		out = append(out, p)
	}
	return out
}

func partitionSizeBytes(paths *Paths, disk string, part string) uint64 {
	path := filepath.Join(paths.SysBlock, disk, part, "size")
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		return 0
	}
	return size * sectorSize
}

func partitionInfo(paths *Paths, part string) (string, string) {
	// Allow calling PartitionInfo with either the full partition name
	// "/dev/sda1" or just "sda1"
	if !strings.HasPrefix(part, "/dev") {
		part = "/dev/" + part
	}

	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	var r io.ReadCloser
	r, err := os.Open(paths.ProcMounts)
	if err != nil {
		return "", ""
	}
	defer util.SafeClose(r)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		entry := parseMountEntry(line)
		if entry == nil || entry.Partition != part {
			continue
		}

		return entry.Mountpoint, entry.FilesystemType
	}
	return "", ""
}

type mountEntry struct {
	Partition      string
	Mountpoint     string
	FilesystemType string
}

func parseMountEntry(line string) *mountEntry {
	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	if line[0] != '/' {
		return nil
	}
	fields := strings.Fields(line)

	if len(fields) < 4 {
		return nil
	}

	// We do some special parsing of the mountpoint, which may contain space,
	// tab and newline characters, encoded into the mount entry line using their
	// octal-to-string representations. From the GNU mtab man pages:
	//
	//   "Therefore these characters are encoded in the files and the getmntent
	//   function takes care of the decoding while reading the entries back in.
	//   '\040' is used to encode a space character, '\011' to encode a tab
	//   character, '\012' to encode a newline character, and '\\' to encode a
	//   backslash."
	mp := fields[1]
	r := strings.NewReplacer(
		"\\011", "\t", "\\012", "\n", "\\040", " ", "\\\\", "\\",
	)
	mp = r.Replace(mp)

	res := &mountEntry{
		Partition:      fields[0],
		Mountpoint:     mp,
		FilesystemType: fields[2],
	}
	return res
}

func diskPartUUID(paths *Paths, disk string, partition string) string {
	info, err := udevInfoPartition(paths, disk, partition)
	if err != nil {
		return util.UNKNOWN
	}

	if pType, ok := info["ID_PART_ENTRY_UUID"]; ok {
		return pType
	}
	return util.UNKNOWN
}

// diskPartTypeUdev gets the partition type from the udev database directly and its only used as fallback when
// the partition is not mounted, so we cannot get the type from paths.ProcMounts from the partitionInfo function
func diskPartTypeUdev(paths *Paths, disk string, partition string) string {
	info, err := udevInfoPartition(paths, disk, partition)
	if err != nil {
		return util.UNKNOWN
	}

	if pType, ok := info["ID_FS_TYPE"]; ok {
		return pType
	}
	return util.UNKNOWN
}

func diskFSLabel(paths *Paths, disk string, partition string) string {
	info, err := udevInfoPartition(paths, disk, partition)
	if err != nil {
		return util.UNKNOWN
	}

	if label, ok := info["ID_FS_LABEL"]; ok {
		return label
	}
	return util.UNKNOWN
}

func udevInfoPartition(paths *Paths, disk string, partition string) (map[string]string, error) {
	// Get device major:minor numbers
	devNo, err := os.ReadFile(filepath.Join(paths.SysBlock, disk, partition, "dev"))
	if err != nil {
		return nil, err
	}
	return UdevInfo(paths, string(devNo))
}

// UdevInfo will return information on udev database about a device number
func UdevInfo(paths *Paths, devNo string) (map[string]string, error) {
	// Look up block device in udev runtime database
	udevID := "b" + strings.TrimSpace(devNo)
	udevBytes, err := os.ReadFile(filepath.Join(paths.RunUdevData, udevID))
	if err != nil {
		return nil, err
	}

	udevInfo := make(map[string]string)
	for _, udevLine := range strings.Split(string(udevBytes), "\n") {
		if strings.HasPrefix(udevLine, "E:") {
			if s := strings.SplitN(udevLine[2:], "=", 2); len(s) == 2 {
				udevInfo[s[0]] = s[1]
			}
		}
	}
	return udevInfo, nil
}
