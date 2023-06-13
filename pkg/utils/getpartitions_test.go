package utils_test

import (
	"github.com/kairos-io/kairos/v2/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PartitionsFromLsblk", func() {
	var lsblkOutput string

	BeforeEach(func() {
		lsblkOutput = `{
   "blockdevices": [
      {
         "name": "mmcblk0",
         "pkname": null,
         "path": "/dev/mmcblk0",
         "fstype": null,
         "mountpoint": null,
         "size": 64088965120,
         "ro": false,
         "label": null
      },{
         "name": "mmcblk0p1",
         "pkname": "mmcblk0",
         "path": "/dev/mmcblk0p1",
         "fstype": "vfat",
         "mountpoint": null,
         "size": 100663296,
         "ro": false,
         "label": "COS_GRUB"
      },{
         "name": "mmcblk0p2",
         "pkname": "mmcblk0",
         "path": "/dev/mmcblk0p2",
         "fstype": "ext4",
         "mountpoint": "/run/initramfs/cos-state",
         "size": 6501171200,
         "ro": false,
         "label": "COS_STATE"
      },{
         "name": "mmcblk0p3",
         "pkname": "mmcblk0",
         "path": "/dev/mmcblk0p3",
         "fstype": "LVM2_member",
         "mountpoint": null,
         "size": 4471128064,
         "ro": false,
         "label": null
      },{
         "name": "mmcblk0p4",
         "pkname": "mmcblk0",
         "path": "/dev/mmcblk0p4",
         "fstype": "ext4",
         "mountpoint": "/usr/local",
         "size": 67108864,
         "ro": false,
         "label": "COS_PERSISTENT"
      },{
         "name": "KairosVG-oem",
         "pkname": "mmcblk0p3",
         "path": "/dev/mapper/KairosVG-oem",
         "fstype": "ext4",
         "mountpoint": "/oem",
         "size": 67108864,
         "ro": false,
         "label": "COS_OEM"
      },{
         "name": "KairosVG-recovery",
         "pkname": "mmcblk0p3",
         "path": "/dev/mapper/KairosVG-recovery",
         "fstype": "ext4",
         "mountpoint": null,
         "size": 4399824896,
         "ro": false,
         "label": "COS_RECOVERY"
      }
   ]
}`
	})

	It("returns all partitions with `Disk` field populated correctly", func() {
		parts, err := utils.PartitionsFromLsblk(lsblkOutput)
		Expect(err).ToNot(HaveOccurred())

		for _, p := range parts {
			Expect(p.Disk).To(Equal("/dev/mmcblk0"))
		}

		Expect(len(parts)).To(Equal(7))
	})
})
