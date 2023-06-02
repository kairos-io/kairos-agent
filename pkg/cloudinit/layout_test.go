package cloudinit

import (
	"fmt"
	"strconv"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos/v2/pkg/partitioner"
	v1 "github.com/kairos-io/kairos/v2/pkg/types/v1"
	v1mock "github.com/kairos-io/kairos/v2/tests/mocks"
	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/twpayne/go-vfs"
	"github.com/twpayne/go-vfs/vfst"
)

var _ = Describe("Layout", Label("layout"), func() {
	// unit test stolen from yip
	var logger v1.Logger
	var stage schema.Stage
	var fs vfs.FS
	var console *cloudInitConsole
	var runner *v1mock.FakeRunner
	var ghwTest v1mock.GhwMock
	var defaultSizeForTest uint
	var device string

	BeforeEach(func() {
		device = "/dev/device"
		defaultSizeForTest = 100
		logger = v1.NewLogger()
		logger.SetLevel(logrus.DebugLevel)
		fs, _, _ = vfst.NewTestFS(map[string]interface{}{device: ""})
		runner = v1mock.NewFakeRunner()
		console = newCloudInitConsole(logger, runner)
		mainDisk := block.Disk{
			Name: "device",
			Partitions: []*block.Partition{
				{
					Name:            "device1",
					FilesystemLabel: "FAKE",
					Type:            "ext4",
					MountPoint:      "/mnt/fake",
					SizeBytes:       0,
				},
			},
		}
		ghwTest = v1mock.GhwMock{}
		ghwTest.AddDisk(mainDisk)
		ghwTest.CreateDevices()
	})

	Describe("Expand partition", Label("expand"), func() {
		BeforeEach(func() {
			partition := "/dev/device1"

			layout := schema.Layout{
				Device: &schema.Device{
					Label: "FAKE",
					Path:  device,
				},
				Expand: &schema.Expand{Size: defaultSizeForTest},
				Parts:  []schema.Partition{},
			}
			stage = schema.Stage{
				Layout: layout,
			}

			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					/*

											Getting free sectors is called by running:
											`parted --script --machine -- /dev/device unit s print`
											And returns the following:
						BYT;
						/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
						1:2048s:206847s:204800s:fat32:EFI System Partition:boot, esp, no_automount;
						2:206848s:239615s:32768s::Microsoft reserved partition:msftres, no_automount;
						3:239616s:2046941183s:2046701568s:ntfs:Basic data partition:msftdata;
						4:2046941184s:2048237567s:1296384s:ntfs::hidden, diag, no_automount;
						5:2048237568s:2050334719s:2097152s:ext4::;
						6:2050334720s:7814035455s:5763700736s:btrfs::;

											So it's the device and its total sectors and picks the last partition and its final sector.
											In this case:
											/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
											^device      ^total sectors
											6:2050334720s:7814035455s:5763700736s:btrfs::;
											^partition    ^end sector

											And you rest (total - end secor of last partition) to know how many free sectors there are.
											At least 20480 sectors are needed to expand properly
					*/
					// Return 1.000.000 total sectors - 1000 used by the partition
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
1:0s:1000s:0s:ext4::;`
					return []byte(rtn), nil
				}
				// removing the first partition and creating a new one
				if command == "parted" && len(args) == 13 {
					if args[6] == "rm" && args[7] == "1" && args[8] == "mkpart" {
						// Create the device
						_, err := fs.Create(partition)
						Expect(err).ToNot(HaveOccurred())
						return nil, err
					}
				}
				return nil, nil
			}
		})

		AfterEach(func() {
			ghwTest.Clean()
		})

		It("Expands latest partition", func() {
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).ToNot(HaveOccurred())
			// This is the sector size that it's going to be passed to parted to increase the new partition size
			// Remember to remove 1 last sector, don't ask me why
			Sectors := partitioner.MiBToSectors(defaultSizeForTest, 512) - 1
			// Check that it tried to delete+create and check the new fs for the new partition and resize it
			Expect(runner.IncludesCmds([][]string{
				{"udevadm", "settle"},
				{"parted", "--script", "--machine", "--", "/dev/device", "unit", "s", "print"},
				{"parted", "--script", "--machine", "--", "/dev/device", "unit", "s", "rm", "1", "mkpart", "part1", "", "0", strconv.Itoa(int(Sectors))},
				{"e2fsck", "-fy", "/dev/device1"},
				{"resize2fs", "/dev/device1"},
			})).ToNot(HaveOccurred())
		})
		It("Fails if there is not enough space", func() {
			// Override runner side effect to return 0 sectors when asked
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
1:0s:1000000s:0s:ext4::;`
					return []byte(rtn), nil
				}
				return nil, nil
			}
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not enough free space"))
		})
		It("Fails if device doesnt exists", func() {
			// Override runner side effect to return 0 sectors when asked
			_ = fs.RemoveAll("/dev/device")
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Target disk not found"))
		})
		It("Fails if new device didnt get created", func() {
			// Override runner side effect to return error when partition is recreated
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					/*

											Getting free sectors is called by running:
											`parted --script --machine -- /dev/device unit s print`
											And returns the following:
						BYT;
						/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
						1:2048s:206847s:204800s:fat32:EFI System Partition:boot, esp, no_automount;
						2:206848s:239615s:32768s::Microsoft reserved partition:msftres, no_automount;
						3:239616s:2046941183s:2046701568s:ntfs:Basic data partition:msftdata;
						4:2046941184s:2048237567s:1296384s:ntfs::hidden, diag, no_automount;
						5:2048237568s:2050334719s:2097152s:ext4::;
						6:2050334720s:7814035455s:5763700736s:btrfs::;

											So it's the device and its total sectors and picks the last partition and its final sector.
											In this case:
											/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
											^device      ^total sectors
											6:2050334720s:7814035455s:5763700736s:btrfs::;
											^partition    ^end sector

											And you rest (total - end secor of last partition) to know how many free sectors there are.
											At least 20480 sectors are needed to expand properly
					*/
					// Return 1.000.000 total sectors - 1000 used by the partition
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
1:0s:1000s:0s:ext4::;`
					return []byte(rtn), nil
				}
				// removing the first partition and creating a new one
				if command == "parted" && len(args) == 13 {
					if args[6] == "rm" && args[7] == "1" && args[8] == "mkpart" {
						// return an error
						return nil, fmt.Errorf("failed")
					}
				}
				return nil, nil
			}
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed"))
		})
		It("Fails if new device didnt get created, even when command didnt return an error", func() {
			// Override runner side effect to return error when partition is recreated
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					/*

											Getting free sectors is called by running:
											`parted --script --machine -- /dev/device unit s print`
											And returns the following:
						BYT;
						/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
						1:2048s:206847s:204800s:fat32:EFI System Partition:boot, esp, no_automount;
						2:206848s:239615s:32768s::Microsoft reserved partition:msftres, no_automount;
						3:239616s:2046941183s:2046701568s:ntfs:Basic data partition:msftdata;
						4:2046941184s:2048237567s:1296384s:ntfs::hidden, diag, no_automount;
						5:2048237568s:2050334719s:2097152s:ext4::;
						6:2050334720s:7814035455s:5763700736s:btrfs::;

											So it's the device and its total sectors and picks the last partition and its final sector.
											In this case:
											/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
											^device      ^total sectors
											6:2050334720s:7814035455s:5763700736s:btrfs::;
											^partition    ^end sector

											And you rest (total - end secor of last partition) to know how many free sectors there are.
											At least 20480 sectors are needed to expand properly
					*/
					// Return 1.000.000 total sectors - 1000 used by the partition
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
1:0s:1000s:0s:ext4::;`
					return []byte(rtn), nil
				}
				// removing the first partition and creating a new one
				if command == "parted" && len(args) == 13 {
					if args[6] == "rm" && args[7] == "1" && args[8] == "mkpart" {
						// Do nothing like the command failed
						return nil, nil
					}
				}
				return nil, nil
			}
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("could not find partition device"))
			Expect(err.Error()).To(ContainSubstring("/dev/device1"))
		})
	})

	Describe("Add partitions", Label("add", "partitions"), func() {
		BeforeEach(func() {
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					/*

											Getting free sectors is called by running:
											`parted --script --machine -- /dev/device unit s print`
											And returns the following:
						BYT;
						/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
						1:2048s:206847s:204800s:fat32:EFI System Partition:boot, esp, no_automount;
						2:206848s:239615s:32768s::Microsoft reserved partition:msftres, no_automount;
						3:239616s:2046941183s:2046701568s:ntfs:Basic data partition:msftdata;
						4:2046941184s:2048237567s:1296384s:ntfs::hidden, diag, no_automount;
						5:2048237568s:2050334719s:2097152s:ext4::;
						6:2050334720s:7814035455s:5763700736s:btrfs::;

											So it's the device and its total sectors and picks the last partition and its final sector.
											In this case:
											/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
											^device      ^total sectors
											6:2050334720s:7814035455s:5763700736s:btrfs::;
											^partition    ^end sector

											And you rest (total - end secor of last partition) to know how many free sectors there are.
											At least 20480 sectors are needed to expand properly
					*/
					// Return 1.000.000 total sectors - 1000 used by the partition
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
1:0s:1000s:0s:ext4::;`
					return []byte(rtn), nil
				}
				return nil, nil
			}
		})
		AfterEach(func() {
			ghwTest.Clean()
		})
		It("Adds one partition", func() {
			fslabel := "jojo"
			fstype := "ext3"
			plabel := "dio"

			layout := schema.Layout{
				Device: &schema.Device{
					Label: "FAKE",
					Path:  device,
				},
				Parts: []schema.Partition{
					{
						Size:       defaultSizeForTest,
						FSLabel:    fslabel,
						FileSystem: fstype,
						PLabel:     plabel,
					},
				},
			}
			stage = schema.Stage{
				Layout: layout,
			}
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					/*

											Getting free sectors is called by running:
											`parted --script --machine -- /dev/device unit s print`
											And returns the following:
						BYT;
						/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
						1:2048s:206847s:204800s:fat32:EFI System Partition:boot, esp, no_automount;
						2:206848s:239615s:32768s::Microsoft reserved partition:msftres, no_automount;
						3:239616s:2046941183s:2046701568s:ntfs:Basic data partition:msftdata;
						4:2046941184s:2048237567s:1296384s:ntfs::hidden, diag, no_automount;
						5:2048237568s:2050334719s:2097152s:ext4::;
						6:2050334720s:7814035455s:5763700736s:btrfs::;

											So it's the device and its total sectors and picks the last partition and its final sector.
											In this case:
											/dev/nvme0n1:7814037168s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
											^device      ^total sectors
											6:2050334720s:7814035455s:5763700736s:btrfs::;
											^partition    ^end sector

											And you rest (total - end secor of last partition) to know how many free sectors there are.
											At least 20480 sectors are needed to expand properly
					*/
					// Return 1.000.000 total sectors - 1000 used by the partition
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;
1:0s:1000s:0s:ext4::;`
					return []byte(rtn), nil
				}
				// removing the first partition and creating a new one
				if command == "parted" && len(args) == 11 {
					// creating partition with our given label and fs type
					if args[6] == "mkpart" && args[7] == plabel && args[8] == fstype {
						logger.Info("Creating part")
						//Create the device
						_, err := fs.Create("/dev/device2")
						Expect(err).ToNot(HaveOccurred())
						return nil, nil
					}
				}
				return nil, nil
			}
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).ToNot(HaveOccurred())
			// Because this is adding a new partition and according to our fake parted the first partitions occupies 1000 sectors
			// We need to sum 1000 sectors to this number to calculate the sectors passed to parted
			// As parted will create the new partition from sector 1001 to MBsToSectors+1001
			Sectors := partitioner.MiBToSectors(defaultSizeForTest, 512) - 1 + 1001
			// Checks that commands to create the new partition were called with the proper fs, size and labels
			Expect(runner.IncludesCmds([][]string{
				{"udevadm", "settle"},
				{"parted", "--script", "--machine", "--", "/dev/device", "unit", "s", "mkpart", plabel, fstype, "1001", strconv.Itoa(int(Sectors))},
				{"mkfs.ext3", "-L", fslabel, "/dev/device2"},
			})).ToNot(HaveOccurred())
		})

		It("Adds multiple partitions", func() {
			partitions := []schema.Partition{
				{
					Size:       100,
					FSLabel:    "fs-label-part-1",
					FileSystem: "ext3",
					PLabel:     "label-part-1",
				},
				{
					Size:       120,
					FSLabel:    "fs-label-part-2",
					FileSystem: "ext4",
					PLabel:     "label-part-2",
				},
			}

			layout := schema.Layout{
				Device: &schema.Device{
					Label: "FAKE",
					Path:  device,
				},
				Parts: partitions,
			}
			stage = schema.Stage{
				Layout: layout,
			}

			type partitionData struct {
				StartSector     int
				EndSector       int
				TotalSectors    int
				PartitionNumber int
				Filesystem      string
				PLabel          string
			}
			createdPartitions := []partitionData{}

			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "parted" && args[4] == "unit" && args[5] == "s" && args[6] == "print" {
					rtn := `
BYT;
/dev/device:1000000s:nvme:512:512:gpt:KINGSTON SFYRD4000G:;`
					for _, p := range createdPartitions {
						rtn += fmt.Sprintf("\n%d:%ds:%ds:%ds:%s::;", p.PartitionNumber, p.StartSector, p.EndSector, p.TotalSectors, p.Filesystem)
					}

					return []byte(rtn), nil
				}

				// removing the first partition and creating a new one
				if command == "parted" && len(args) == 11 {
					// creating partition with our given label and fs type
					if args[6] == "mkpart" {
						endSector, err := strconv.Atoi(args[10])
						Expect(err).ToNot(HaveOccurred())
						startSector, err := strconv.Atoi(args[9])
						Expect(err).ToNot(HaveOccurred())

						newPart := partitionData{
							StartSector:     startSector,
							EndSector:       endSector,
							TotalSectors:    endSector - startSector,
							PartitionNumber: len(createdPartitions) + 1,
							Filesystem:      args[8],
							PLabel:          args[7],
						}

						createdPartitions = append(createdPartitions, newPart)
						_, err = fs.Create(fmt.Sprintf("/dev/device%d", newPart.PartitionNumber))
						Expect(err).ToNot(HaveOccurred())
						return nil, nil
					}
				}
				return nil, nil
			}
			err := layoutPlugin(logger, stage, fs, console)
			Expect(err).ToNot(HaveOccurred())

			Expect(len(createdPartitions)).To(Equal(len(partitions)))
			// Checks that commands to create the new partition were called with the proper fs, size and labels
			partedCmds := [][]string{}
			for _, p := range createdPartitions {
				partedCmds = append(partedCmds, []string{
					"parted", "--script", "--machine", "--", "/dev/device", "unit", "s", "mkpart", p.PLabel, p.Filesystem, strconv.Itoa(p.StartSector), strconv.Itoa(p.EndSector),
				})
				partedCmds = append(partedCmds, []string{
					fmt.Sprintf("mkfs.%s", p.Filesystem), "-L", fmt.Sprintf("fs-%s", p.PLabel), fmt.Sprintf("/dev/device%d", p.PartitionNumber),
				})
			}

			Expect(runner.IncludesCmds(partedCmds)).ToNot(HaveOccurred())
		})
	})
})
