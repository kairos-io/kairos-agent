/*
Copyright © 2022 SUSE LLC

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
package action_test

import (
	"fmt"
	"testing"

	"github.com/kairos-io/kairos/v2/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestActionSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Actions test suite")
}

func lsblkMockOutput() []byte {
	return []byte(fmt.Sprintf(
		`{
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
						"mountpoint": "%s",
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
						"mountpoint": "%s",
						"size": 4399824896,
						"ro": false,
						"label": "COS_RECOVERY"
					}
			]
		}`, constants.RunningStateDir, constants.LiveDir))
}
