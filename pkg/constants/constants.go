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

package constants

import (
	"fmt"
	"os"
)

const (
	GrubConf                = "/etc/cos/grub.cfg"
	GrubOEMEnv              = "grub_oem_env"
	GrubDefEntry            = "Kairos"
	DefaultTty              = "tty1"
	BiosPartName            = "bios"
	EfiLabel                = "COS_GRUB"
	EfiPartName             = "efi"
	ActiveLabel             = "COS_ACTIVE"
	PassiveLabel            = "COS_PASSIVE"
	SystemLabel             = "COS_SYSTEM"
	RecoveryLabel           = "COS_RECOVERY"
	RecoveryPartName        = "recovery"
	StateLabel              = "COS_STATE"
	StatePartName           = "state"
	InstallStateFile        = "state.yaml"
	PersistentLabel         = "COS_PERSISTENT"
	PersistentPartName      = "persistent"
	OEMLabel                = "COS_OEM"
	OEMPartName             = "oem"
	ISOLabel                = "COS_LIVE"
	MountBinary             = "/usr/bin/mount"
	EfiDevice               = "/sys/firmware/efi"
	LinuxFs                 = "ext4"
	LinuxImgFs              = "ext2"
	SquashFs                = "squashfs"
	EfiFs                   = "vfat"
	EfiSize                 = uint(64)
	OEMSize                 = uint(64)
	StateSize               = uint(15360)
	RecoverySize            = uint(8192)
	PersistentSize          = uint(0)
	BiosSize                = uint(1)
	ImgSize                 = uint(3072)
	HTTPTimeout             = 60
	LiveDir                 = "/run/initramfs/live"
	RecoveryDir             = "/run/cos/recovery"
	StateDir                = "/run/cos/state"
	OEMDir                  = "/run/cos/oem"
	PersistentDir           = "/run/cos/persistent"
	ActiveDir               = "/run/cos/active"
	TransitionDir           = "/run/cos/transition"
	EfiDir                  = "/run/cos/efi"
	RecoverySquashFile      = "recovery.squashfs"
	IsoRootFile             = "rootfs.squashfs"
	IsoEFIPath              = "/boot/uefi.img"
	ActiveImgFile           = "active.img"
	PassiveImgFile          = "passive.img"
	RecoveryImgFile         = "recovery.img"
	IsoBaseTree             = "/run/rootfsbase"
	AfterInstallChrootHook  = "after-install-chroot"
	AfterInstallHook        = "after-install"
	BeforeInstallHook       = "before-install"
	AfterResetChrootHook    = "after-reset-chroot"
	AfterResetHook          = "after-reset"
	BeforeResetHook         = "before-reset"
	AfterUpgradeChrootHook  = "after-upgrade-chroot"
	AfterUpgradeHook        = "after-upgrade"
	BeforeUpgradeHook       = "before-upgrade"
	LuetDefaultRepoURI      = "quay.io/costoolkit/releases-green"
	LuetDefaultRepoPrio     = 90
	TransitionImgFile       = "transition.img"
	RunningStateDir         = "/run/initramfs/cos-state" // TODO: converge this constant with StateDir/RecoveryDir in dracut module from cos-toolkit
	RunningRecoveryStateDir = "/run/initramfs/isoscan"   // TODO: converge this constant with StateDir/RecoveryDir in dracut module from cos-toolkit
	ActiveImgName           = "active"
	PassiveImgName          = "passive"
	RecoveryImgName         = "recovery"
	GPT                     = "gpt"
	BuildImgName            = "elemental"
	UsrLocalPath            = "/usr/local"
	OEMPath                 = "/oem"

	// SELinux targeted policy paths
	SELinuxTargetedPath        = "/etc/selinux/targeted"
	SELinuxTargetedContextFile = SELinuxTargetedPath + "/contexts/files/file_contexts"
	SELinuxTargetedPolicyPath  = SELinuxTargetedPath + "/policy"

	// These paths are arbitrary but coupled to grub.cfg
	IsoKernelPath = "/boot/kernel"
	IsoInitrdPath = "/boot/initrd"

	// Default directory and file fileModes
	DirPerm        = os.ModeDir | os.ModePerm
	FilePerm       = 0666
	NoWriteDirPerm = 0555 | os.ModeDir
	TempDirPerm    = os.ModePerm | os.ModeSticky | os.ModeDir

	// Eject script
	EjectScript = "#!/bin/sh\n/usr/bin/eject -rmF"

	ArchAmd64  = "amd64"
	Archx86    = "x86_64"
	ArchArm64  = "arm64"
	SignedShim = "shim.efi"

	Rsync = "rsync"
)

func GetCloudInitPaths() []string {
	return []string{"/system/oem", "/oem/", "/usr/local/cloud-config/"}
}

// GetDefaultSquashfsOptions returns the default options to use when creating a squashfs
func GetDefaultSquashfsOptions() []string {
	return []string{"-b", "1024k"}
}

func GetDefaultSquashfsCompressionOptions() []string {
	return []string{"-comp", "gzip"}
}

func GetBuildDiskDefaultPackages() map[string]string {
	return map[string]string{
		"channel:system/grub2-efi-image": "efi",
		"channel:system/grub2-config":    "root",
		"channel:system/grub2-artifacts": "root/grub2",
		"channel:recovery/cos-img":       "root/cOS",
	}
}

func GetGrubFilePaths(arch string) []string {
	var archPath string
	switch arch {
	case ArchArm64:
		archPath = "aarch64"
	default:
		archPath = Archx86
	}
	return []string{
		fmt.Sprintf("/usr/share/efi/%s/grub.efi", archPath),
		fmt.Sprintf("/usr/share/efi/%s/%s", archPath, SignedShim),
	}
}

func GetFallBackEfi(arch string) string {
	switch arch {
	case ArchArm64:
		return "bootaa64.efi"
	default:
		return "bootx64.efi"
	}
}

// GetGrubFonts returns the default font files for grub
func GetGrubFonts() []string {
	return []string{"ascii.pf2", "euro.pf2", "unicode.pf2"}
}

// GetGrubModules returns the default module files for grub
func GetGrubModules() []string {
	return []string{"loopback.mod", "squash4.mod", "xzio.mod", "gzio.mod"}
}
