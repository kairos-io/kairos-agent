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

package v1_test

import (
	sdkTypes "github.com/kairos-io/kairos-sdk/types"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Types", Label("types", "config"), func() {
	Describe("ElementalPartitions", func() {
		var p sdkTypes.PartitionList
		var ep v1.ElementalPartitions
		BeforeEach(func() {
			ep = v1.ElementalPartitions{}
			p = sdkTypes.PartitionList{
				&sdkTypes.Partition{
					FilesystemLabel: "COS_OEM",
					Size:            0,
					Name:            "oem",
					FS:              "",
					Flags:           nil,
					MountPoint:      "/some/path/nested",
					Path:            "",
					Disk:            "",
				},
				&sdkTypes.Partition{
					FilesystemLabel: "COS_CUSTOM",
					Size:            0,
					Name:            "persistent",
					FS:              "",
					Flags:           nil,
					MountPoint:      "/some/path",
					Path:            "",
					Disk:            "",
				},
				&sdkTypes.Partition{
					FilesystemLabel: "SOMETHING",
					Size:            0,
					Name:            "somethingelse",
					FS:              "",
					Flags:           nil,
					MountPoint:      "",
					Path:            "",
					Disk:            "",
				},
			}
		})
		It("sets firmware partitions on efi", func() {
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(v1.EFI, v1.GPT)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ep.EFI != nil && ep.BIOS == nil).To(BeTrue())
		})
		It("sets firmware partitions on bios", func() {
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(v1.BIOS, v1.GPT)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ep.EFI == nil && ep.BIOS != nil).To(BeTrue())
		})
		It("sets firmware partitions on msdos", func() {
			ep.State = &sdkTypes.Partition{}
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(v1.BIOS, v1.MSDOS)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			Expect(ep.State.Flags != nil && ep.State.Flags[0] == "boot").To(BeTrue())
		})
		It("fails to set firmware partitions of state is not defined on msdos", func() {
			Expect(ep.EFI == nil && ep.BIOS == nil).To(BeTrue())
			err := ep.SetFirmwarePartitions(v1.BIOS, v1.MSDOS)
			Expect(err).Should(HaveOccurred())
		})
		It("initializes an ElementalPartitions from a PartitionList", func() {
			ep := v1.NewElementalPartitionsFromList(p)
			Expect(ep.Persistent != nil).To(BeTrue())
			Expect(ep.OEM != nil).To(BeTrue())
			Expect(ep.BIOS == nil).To(BeTrue())
			Expect(ep.EFI == nil).To(BeTrue())
			Expect(ep.State == nil).To(BeTrue())
			Expect(ep.Recovery == nil).To(BeTrue())
		})
		Describe("returns a partition list by install order", func() {
			It("with no extra parts", func() {
				ep := v1.NewElementalPartitionsFromList(p)
				lst := ep.PartitionsByInstallOrder([]*sdkTypes.Partition{})
				Expect(len(lst)).To(Equal(2))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
			})
			It("with extra parts with size > 0", func() {
				ep := v1.NewElementalPartitionsFromList(p)
				var extraParts []*sdkTypes.Partition
				extraParts = append(extraParts, &sdkTypes.Partition{Name: "extra", Size: 5})

				lst := ep.PartitionsByInstallOrder(extraParts)
				Expect(len(lst)).To(Equal(3))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "extra").To(BeTrue())
				Expect(lst[2].Name == "persistent").To(BeTrue())
			})
			It("with extra part with size == 0 and persistent.Size == 0", func() {
				ep := v1.NewElementalPartitionsFromList(p)
				var extraParts []*sdkTypes.Partition
				extraParts = append(extraParts, &sdkTypes.Partition{Name: "extra", Size: 0})
				lst := ep.PartitionsByInstallOrder(extraParts)
				// Should ignore the wrong partition had have the persistent over it
				Expect(len(lst)).To(Equal(2))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
			})
			It("with extra part with size == 0 and persistent.Size > 0", func() {
				ep := v1.NewElementalPartitionsFromList(p)
				ep.Persistent.Size = 10
				var extraParts []*sdkTypes.Partition
				extraParts = append(extraParts, &sdkTypes.Partition{Name: "extra", FilesystemLabel: "LABEL", Size: 0})
				lst := ep.PartitionsByInstallOrder(extraParts)
				// Will have our size == 0 partition the latest
				Expect(len(lst)).To(Equal(3))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
				Expect(lst[2].Name == "extra").To(BeTrue())
			})
			It("with several extra parts with size == 0 and persistent.Size > 0", func() {
				ep := v1.NewElementalPartitionsFromList(p)
				ep.Persistent.Size = 10
				var extraParts []*sdkTypes.Partition
				extraParts = append(extraParts, &sdkTypes.Partition{Name: "extra1", Size: 0})
				extraParts = append(extraParts, &sdkTypes.Partition{Name: "extra2", Size: 0})
				lst := ep.PartitionsByInstallOrder(extraParts)
				// Should ignore the wrong partition had have the first partition with size 0 added last
				Expect(len(lst)).To(Equal(3))
				Expect(lst[0].Name == "oem").To(BeTrue())
				Expect(lst[1].Name == "persistent").To(BeTrue())
				Expect(lst[2].Name == "extra1").To(BeTrue())
			})
		})

		It("returns a partition list by mount order", func() {
			ep := v1.NewElementalPartitionsFromList(p)
			lst := ep.PartitionsByMountPoint(false)
			Expect(len(lst)).To(Equal(2))
			Expect(lst[0].Name == "persistent").To(BeTrue())
			Expect(lst[1].Name == "oem").To(BeTrue())
		})
		It("returns a partition list by mount reverse order", func() {
			ep := v1.NewElementalPartitionsFromList(p)
			lst := ep.PartitionsByMountPoint(true)
			Expect(len(lst)).To(Equal(2))
			Expect(lst[0].Name == "oem").To(BeTrue())
			Expect(lst[1].Name == "persistent").To(BeTrue())
		})
	})
	Describe("PartitionList", func() {
		var p sdkTypes.PartitionList
		BeforeEach(func() {
			p = sdkTypes.PartitionList{
				&sdkTypes.Partition{
					FilesystemLabel: "ONE",
					Size:            0,
					Name:            "one",
					FS:              "",
					Flags:           nil,
					MountPoint:      "",
					Path:            "",
					Disk:            "",
				},
				&sdkTypes.Partition{
					FilesystemLabel: "TWO",
					Size:            0,
					Name:            "two",
					FS:              "",
					Flags:           nil,
					MountPoint:      "",
					Path:            "",
					Disk:            "",
				},
			}
		})
		It("returns partitions by name", func() {
			Expect(v1.GetByName("two", p)).To(Equal(&sdkTypes.Partition{
				FilesystemLabel: "TWO",
				Size:            0,
				Name:            "two",
				FS:              "",
				Flags:           nil,
				MountPoint:      "",
				Path:            "",
				Disk:            "",
			}))
		})
		It("returns nil if partiton name not found", func() {
			Expect(v1.GetByName("nonexistent", p)).To(BeNil())
		})
		It("returns partitions by filesystem label", func() {
			Expect(v1.GetByLabel("TWO", p)).To(Equal(&sdkTypes.Partition{
				FilesystemLabel: "TWO",
				Size:            0,
				Name:            "two",
				FS:              "",
				Flags:           nil,
				MountPoint:      "",
				Path:            "",
				Disk:            "",
			}))
		})
		It("returns nil if filesystem label not found", func() {
			Expect(v1.GetByName("nonexistent", p)).To(BeNil())
		})
	})
	Describe("Specs", func() {
		Describe("InstallSpec sanitize", func() {
			var spec v1.InstallSpec
			BeforeEach(func() {
				spec = v1.InstallSpec{
					Active: v1.Image{
						Source: v1.NewEmptySrc(),
					},
					Recovery: v1.Image{
						Source: v1.NewEmptySrc(),
					},
					Passive: v1.Image{
						Source: v1.NewEmptySrc(),
					},
					Partitions: v1.ElementalPartitions{
						OEM:        &sdkTypes.Partition{},
						Recovery:   &sdkTypes.Partition{},
						Persistent: &sdkTypes.Partition{},
					},
					ExtraPartitions: sdkTypes.PartitionList{},
				}
			})
			It("fails with empty source", func() {
				err := spec.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined system source to install"))
			})
			It("fails with missing state partition", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				err := spec.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined state partition"))
			})
			It("passes if state and source are ready", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				err := spec.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})
			It("fills the spec with defaults (BIOS)", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				spec.Firmware = constants.BiosPartName
				spec.PartTable = constants.GPT
				err := spec.Sanitize()
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Recovery.File).ToNot(BeEmpty())
				Expect(spec.Recovery.File).To(Equal(filepath.Join(constants.RecoveryDir, "cOS", constants.RecoveryImgFile)))
				Expect(spec.Partitions.EFI).To(BeNil())
				Expect(spec.Partitions.BIOS).ToNot(BeNil())
				Expect(spec.Partitions.BIOS.Flags).To(ContainElement("bios_grub"))
				Expect(spec.Partitions.OEM.FilesystemLabel).To(Equal(constants.OEMLabel))
				Expect(spec.Partitions.OEM.Name).To(Equal(constants.OEMPartName))
				Expect(spec.Partitions.Recovery.FilesystemLabel).To(Equal(constants.RecoveryLabel))
				Expect(spec.Partitions.Recovery.Name).To(Equal(constants.RecoveryPartName))
				Expect(spec.Partitions.Persistent.FilesystemLabel).To(Equal(constants.PersistentLabel))
				Expect(spec.Partitions.Persistent.Name).To(Equal(constants.PersistentPartName))
				Expect(spec.Partitions.State.FilesystemLabel).To(Equal(constants.StateLabel))
				Expect(spec.Partitions.State.Name).To(Equal(constants.StatePartName))
			})
			It("fills the spec with defaults (EFI)", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				spec.Firmware = constants.EfiPartName
				spec.PartTable = constants.GPT
				err := spec.Sanitize()
				Expect(err).ToNot(HaveOccurred())
				Expect(spec.Recovery.File).ToNot(BeEmpty())
				Expect(spec.Recovery.File).To(Equal(filepath.Join(constants.RecoveryDir, "cOS", constants.RecoveryImgFile)))
				Expect(spec.Partitions.BIOS).To(BeNil())
				Expect(spec.Partitions.EFI).ToNot(BeNil())
				Expect(spec.Partitions.EFI.FilesystemLabel).To(Equal(constants.EfiLabel))
				Expect(spec.Partitions.EFI.MountPoint).To(Equal(constants.EfiDir))
				Expect(spec.Partitions.EFI.Size).To(Equal(constants.EfiSize))
				Expect(spec.Partitions.EFI.FS).To(Equal(constants.EfiFs))
				Expect(spec.Partitions.EFI.Flags).To(ContainElement("esp"))
				Expect(spec.Partitions.OEM.FilesystemLabel).To(Equal(constants.OEMLabel))
				Expect(spec.Partitions.OEM.Name).To(Equal(constants.OEMPartName))
				Expect(spec.Partitions.Recovery.FilesystemLabel).To(Equal(constants.RecoveryLabel))
				Expect(spec.Partitions.Recovery.Name).To(Equal(constants.RecoveryPartName))
				Expect(spec.Partitions.Persistent.FilesystemLabel).To(Equal(constants.PersistentLabel))
				Expect(spec.Partitions.Persistent.Name).To(Equal(constants.PersistentPartName))
				Expect(spec.Partitions.State.FilesystemLabel).To(Equal(constants.StateLabel))
				Expect(spec.Partitions.State.Name).To(Equal(constants.StatePartName))
			})
			It("Cannot add extra partitions with 0 size + persistent with 0 size", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				spec.Partitions.Persistent = &sdkTypes.Partition{
					Size: 0,
				}
				spec.Firmware = constants.BiosPartName
				spec.PartTable = constants.GPT
				spec.ExtraPartitions = sdkTypes.PartitionList{
					&sdkTypes.Partition{
						Size: 0,
					},
				}
				err := spec.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("both persistent partition and extra partitions have size set to 0"))
			})
			It("Cannot add more than 1 extra partition with 0 size", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				spec.Partitions.Persistent = &sdkTypes.Partition{
					Size: 100,
				}
				spec.Firmware = constants.BiosPartName
				spec.PartTable = constants.GPT
				spec.ExtraPartitions = sdkTypes.PartitionList{
					&sdkTypes.Partition{
						Size: 0,
					},
					&sdkTypes.Partition{
						Size: 0,
					},
				}
				err := spec.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("more than one extra partition has its size set to 0"))
			})
			It("Can add 1 extra partition with 0 size", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				spec.Partitions.Persistent = &sdkTypes.Partition{
					Size: 100,
				}
				spec.Firmware = constants.BiosPartName
				spec.PartTable = constants.GPT
				spec.ExtraPartitions = sdkTypes.PartitionList{
					&sdkTypes.Partition{
						Size: 0,
					},
				}
				err := spec.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})
		})
		Describe("ResetSpec sanitize", func() {
			var spec v1.ResetSpec
			BeforeEach(func() {
				spec = v1.ResetSpec{
					Active: v1.Image{
						Source: v1.NewEmptySrc(),
					},
				}
			})
			It("fails with empty source", func() {
				err := spec.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined system source to reset"))
			})
			It("fails with missing state partition", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				err := spec.Sanitize()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("undefined state partition"))
			})
			It("passes if state and source are ready", func() {
				spec.Active.Source = v1.NewFileSrc("/tmp")
				spec.Partitions.State = &sdkTypes.Partition{
					MountPoint: "/tmp",
				}
				spec.Partitions.OEM = &sdkTypes.Partition{}
				spec.Partitions.Recovery = &sdkTypes.Partition{}
				spec.Partitions.Persistent = &sdkTypes.Partition{}
				err := spec.Sanitize()
				Expect(err).ToNot(HaveOccurred())
			})

		})
		Describe("UpgradeSpec sanitize", func() {
			var spec v1.UpgradeSpec
			BeforeEach(func() {
				spec = v1.UpgradeSpec{
					Active: v1.Image{
						Source: v1.NewEmptySrc(),
					},
					Recovery: v1.Image{
						Source: v1.NewEmptySrc(),
					},
				}
			})
			Describe("Active upgrade", func() {
				It("fails with empty source", func() {
					err := spec.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(constants.UpgradeNoSourceError))
				})
				It("fails with missing state partition", func() {
					spec.Active.Source = v1.NewFileSrc("/tmp")
					err := spec.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("undefined state partition"))
				})
				It("passes if state and source are ready", func() {
					spec.Active.Source = v1.NewFileSrc("/tmp")
					spec.Partitions.State = &sdkTypes.Partition{
						MountPoint: "/tmp",
					}
					err := spec.Sanitize()
					Expect(err).ToNot(HaveOccurred())
				})
			})
			Describe("Recovery upgrade", func() {
				BeforeEach(func() {
					spec.Entry = constants.BootEntryRecovery
				})
				It("fails with empty source", func() {
					err := spec.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(constants.UpgradeNoSourceError))
				})
				It("fails with missing recovery partition", func() {
					spec.Recovery.Source = v1.NewFileSrc("/tmp")
					err := spec.Sanitize()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("undefined recovery partition"))
				})
				It("passes if state and source are ready", func() {
					spec.Recovery.Source = v1.NewFileSrc("/tmp")
					spec.Partitions.Recovery = &sdkTypes.Partition{
						MountPoint: "/tmp",
					}
					err := spec.Sanitize()
					Expect(err).ToNot(HaveOccurred())
				})
			})

		})
	})
})
