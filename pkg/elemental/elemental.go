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

package elemental

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/kairos-io/kairos-sdk/types"

	"github.com/containerd/containerd/archive"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/partitioner"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/loop"
)

// Elemental is the struct meant to self-contain most utils and actions related to Elemental, like installing or applying selinux
type Elemental struct {
	config *agentConfig.Config
}

func NewElemental(config *agentConfig.Config) *Elemental {
	return &Elemental{
		config: config,
	}
}

// Deprecated: Use FormatPartition instead
func (e *Elemental) FormatPartition(part *types.Partition, opts ...string) error {
	return FormatPartition(e.config, part, opts...)
}

// FormatPartition will format an already existing partition
// Decoupled from Elemental to allow for use in other contexts
func FormatPartition(config *agentConfig.Config, part *types.Partition, opts ...string) error {
	return partitioner.FormatDevice(config.Logger, config.Runner, part.Path, part.FS, part.FilesystemLabel, opts...)
}

// Deprecated: Use PartitionAndFormatDevice instead
func (e *Elemental) PartitionAndFormatDevice(i v1.SharedInstallSpec) error {
	return PartitionAndFormatDevice(e.config, i)
}

// PartitionAndFormatDevice creates a new empty partition table on target disk
// and applies the configured disk layout by creating and formatting all
// required partitions
func PartitionAndFormatDevice(config *agentConfig.Config, i v1.SharedInstallSpec) error {
	if _, err := os.Stat(i.GetTarget()); os.IsNotExist(err) {
		config.Logger.Errorf("Disk %s does not exist", i.GetTarget())
		return fmt.Errorf("disk %s does not exist", i.GetTarget())
	}

	disk, err := partitioner.NewDisk(i.GetTarget(), partitioner.WithLogger(config.Logger))
	if err != nil {
		return err
	}

	config.Logger.Infof("Partitioning device...")
	err = disk.NewPartitionTable(i.GetPartTable(), i.GetPartitions().PartitionsByInstallOrder(i.GetExtraPartitions()))
	if err != nil {
		config.Logger.Errorf("Failed creating new partition table: %s", err)
		return err
	}

	err = disk.ReReadPartitionTable()
	if err != nil {
		config.Logger.Errorf("Reread table: %s", err)
		return err
	}

	table, err := disk.GetPartitionTable()
	if err != nil {
		config.Logger.Errorf("table: %s", err)
		return err
	}
	err = disk.Close()
	if err != nil {
		config.Logger.Errorf("Close disk: %s", err)
	}
	// Sync changes
	syscall.Sync()
	// Trigger udevadm to refresh devices
	_, err = config.Runner.Run("udevadm", "trigger")
	if err != nil {
		config.Logger.Errorf("Udevadm trigger failed: %s", err)
	}
	_, err = config.Runner.Run("udevadm", "settle")
	if err != nil {
		config.Logger.Errorf("Udevadm settle failed: %s", err)
	}
	// Partitions are in order so we can format them via that
	for _, p := range table.GetPartitions() {
		for _, configPart := range i.GetPartitions().PartitionsByInstallOrder(i.GetExtraPartitions()) {
			if configPart.Name == cnst.BiosPartName {
				// Grub partition on non-EFI is not formatted. Grub is directly installed on it
				continue
			}
			// we have to match the Fs it was asked with the partition in the system
			if p.(*gpt.Partition).Name == configPart.Name {
				config.Logger.Debugf("Formatting partition: %s", configPart.FilesystemLabel)
				// Get full partition path by the /dev/disk/by-partlabel/ facility
				// So we don't need to infer the actual device under it but get udev to tell us
				// So this works for "normal" devices that have the "expected" partitions (i.e. /dev/sda has /dev/sda1, /dev/sda2)
				// And "weird" devices that have special subdevices like mmc or nvme
				// i.e. /dev/mmcblk0 has /dev/mmcblk0p1, /dev/mmcblk0p2
				device, err := filepath.EvalSymlinks(fmt.Sprintf("/dev/disk/by-partlabel/%s", configPart.Name))
				if err != nil {
					config.Logger.Errorf("Failed finding partition %s by partition label: %s", configPart.FilesystemLabel, err)
				}
				err = partitioner.FormatDevice(config.Logger, config.Runner, device, configPart.FS, configPart.FilesystemLabel)
				if err != nil {
					config.Logger.Errorf("Failed formatting partition: %s", err)
					return err
				}
				syscall.Sync()
			}
		}
	}
	return nil
}

// Deprecated: Use MountPartitions instead
func (e Elemental) MountPartitions(parts types.PartitionList) error {
	return MountPartitions(e.config, parts)
}

// MountPartitions mounts configured partitions. Partitions with an unset mountpoint are not mounted.
// Note umounts must be handled by caller logic.
func MountPartitions(config *agentConfig.Config, parts types.PartitionList) error {
	config.Logger.Infof("Mounting disk partitions")
	var err error

	for _, part := range parts {
		if part.MountPoint != "" {
			err = MountPartition(config, part, "rw")
			if err != nil {
				_ = UnmountPartitions(config, parts)
				return err
			}
		}
	}

	return err
}

// Deprecated: Use UnmountPartitions instead
func (e Elemental) UnmountPartitions(parts types.PartitionList) error {
	return UnmountPartitions(e.config, parts)
}

// UnmountPartitions unmounts configured partitiosn. Partitions with an unset mountpoint are not unmounted.
func UnmountPartitions(config *agentConfig.Config, parts types.PartitionList) error {
	config.Logger.Infof("Unmounting disk partitions")
	var err error
	errMsg := ""
	failure := false

	// If there is an early error we still try to unmount other partitions
	for _, part := range parts {
		if part.MountPoint != "" {
			err = UnmountPartition(config, part)
			if err != nil {
				errMsg += fmt.Sprintf("Failed to unmount %s\n Error: %s\n", part.MountPoint, err.Error())
				failure = true
			}
		}
	}
	if failure {
		return errors.New(errMsg)
	}
	return nil
}

// Deprecated: Use MountRWPartition instead
func (e Elemental) MountRWPartition(part *types.Partition) (umount func() error, err error) {
	return MountRWPartition(e.config, part)
}

// MountRWPartition mounts, or remounts if needed, a partition with RW permissions
func MountRWPartition(config *agentConfig.Config, part *types.Partition) (umount func() error, err error) {
	if mnt, _ := utils.IsMounted(config, part); mnt {
		err = MountPartition(config, part, "remount", "rw")
		if err != nil {
			config.Logger.Errorf("failed mounting %s partition: %v", part.Name, err)
			return nil, err
		}
		umount = func() error {
			config.Logger.Debugf("Remounting partition %s as read-only", part.FilesystemLabel)
			// Remount with bind and read-only options so we avoid in use errors
			return config.Syscall.Mount(part.MountPoint, part.MountPoint, "", syscall.MS_REMOUNT|syscall.MS_RDONLY|syscall.MS_BIND, "")
		}
	} else {
		err = MountPartition(config, part, "rw")
		if err != nil {
			config.Logger.Errorf("failed mounting %s partition: %v", part.Name, err)
			return nil, err
		}
		umount = func() error { return UnmountPartition(config, part) }
	}
	return umount, nil
}

// Deprecated: Use MountPartition instead
func (e Elemental) MountPartition(part *types.Partition, opts ...string) error {
	return MountPartition(e.config, part, opts...)
}

// MountPartition mounts a partition with the given mount options
func MountPartition(config *agentConfig.Config, part *types.Partition, opts ...string) error {
	config.Logger.Debugf("Mounting partition %s", part.FilesystemLabel)
	err := fsutils.MkdirAll(config.Fs, part.MountPoint, cnst.DirPerm)
	if err != nil {
		return err
	}
	if part.Path == "" {
		// Lets error out only after 10 attempts to find the device
		device, err := utils.GetDeviceByLabel(config, part.FilesystemLabel, 10)
		if err != nil {
			config.Logger.Errorf("Could not find a device with label %s", part.FilesystemLabel)
			return err
		}
		part.Path = device
	}
	err = config.Mounter.Mount(part.Path, part.MountPoint, "auto", opts)
	if err != nil {
		config.Logger.Errorf("Failed mounting device %s with label %s", part.Path, part.FilesystemLabel)
		return err
	}
	return nil
}

// Deprecated: Use UnmountPartition instead
func (e Elemental) UnmountPartition(part *types.Partition) error {
	return UnmountPartition(e.config, part)
}

// UnmountPartition unmounts the given partition or does nothing if not mounted
func UnmountPartition(config *agentConfig.Config, part *types.Partition) error {
	if mnt, _ := utils.IsMounted(config, part); !mnt {
		config.Logger.Debugf("Not unmounting partition, %s doesn't look like mountpoint", part.MountPoint)
		return nil
	}
	config.Logger.Debugf("Unmounting partition %s", part.FilesystemLabel)
	return config.Mounter.Unmount(part.MountPoint)
}

// Deprecated: Use MountImage instead
func (e Elemental) MountImage(img *v1.Image, opts ...string) error {
	return MountImage(e.config, img, opts...)
}

// MountImage mounts an image with the given mount options
func MountImage(config *agentConfig.Config, img *v1.Image, opts ...string) error {
	config.Logger.Debugf("Mounting image %s", img.Label)
	err := fsutils.MkdirAll(config.Fs, img.MountPoint, cnst.DirPerm)
	if err != nil {
		return err
	}
	loopDevice, err := loop.Loop(img, config)
	if err != nil {
		return err
	}

	err = config.Mounter.Mount(loopDevice, img.MountPoint, "auto", opts)
	if err != nil {
		return err
	}

	// Store the loop device so we can later detach it
	img.LoopDevice = loopDevice
	return nil
}

// Deprecated: Use UnmountImage instead
func (e Elemental) UnmountImage(img *v1.Image) error {
	return UnmountImage(e.config, img)
}

// UnmountImage unmounts the given image or does nothing if not mounted
func UnmountImage(config *agentConfig.Config, img *v1.Image) error {
	// Using IsLikelyNotMountPoint seams to be safe as we are not checking
	// for bind mounts here
	if notMnt, _ := config.Mounter.IsLikelyNotMountPoint(img.MountPoint); notMnt {
		config.Logger.Debugf("Not unmounting image, %s doesn't look like mountpoint", img.MountPoint)
		return nil
	}

	config.Logger.Debugf("Unmounting image %s", img.Label)
	err := config.Mounter.Unmount(img.MountPoint)
	if err != nil {
		return err
	}
	err = loop.Unloop(img.LoopDevice, config)
	if err != nil {
		return err
	}
	img.LoopDevice = ""
	return err
}

// Deprecated: Use CreateFileSystemImage instead
func (e Elemental) CreateFileSystemImage(img *v1.Image) error {
	return CreateFileSystemImage(e.config, img)
}

// CreateFileSystemImage creates the image file for config.target
func CreateFileSystemImage(config *agentConfig.Config, img *v1.Image) error {
	config.Logger.Infof("Creating file system image %s with size %dMb", img.File, img.Size)
	err := fsutils.MkdirAll(config.Fs, filepath.Dir(img.File), cnst.DirPerm)
	if err != nil {
		return err
	}
	actImg, err := config.Fs.Create(img.File)
	if err != nil {
		return err
	}

	err = actImg.Truncate(int64(img.Size * 1024 * 1024))
	if err != nil {
		actImg.Close()
		_ = config.Fs.RemoveAll(img.File)
		return err
	}
	err = actImg.Close()
	if err != nil {
		_ = config.Fs.RemoveAll(img.File)
		return err
	}

	mkfs := partitioner.NewMkfsCall(img.File, img.FS, img.Label, config.Runner)
	_, err = mkfs.Apply()
	if err != nil {
		_ = config.Fs.RemoveAll(img.File)
		return err
	}
	return nil
}

// Deprecated: Use DeployImage instead
func (e *Elemental) DeployImage(img *v1.Image, leaveMounted bool) (info interface{}, err error) {
	return DeployImage(e.config, img, leaveMounted)
}

// DeployImage will deploy the given image into the target. This method
// creates the filesystem image file, mounts it and unmounts it as needed.
// Creates the default system dirs by default (/sys,/proc,/dev, etc...)
func DeployImage(config *agentConfig.Config, img *v1.Image, leaveMounted bool) (info interface{}, err error) {
	return deployImage(config, img, leaveMounted, true)
}

// Deprecated: Use DeployImageNodirs instead
func (e *Elemental) DeployImageNodirs(img *v1.Image, leaveMounted bool) (info interface{}, err error) {
	return DeployImageNodirs(e.config, img, leaveMounted)
}

// DeployImageNodirs will deploy the given image into the target. This method
// creates the filesystem image file, mounts it and unmounts it as needed.
// Does not create the default system dirs so it can be used to create generic images from any source
func DeployImageNodirs(config *agentConfig.Config, img *v1.Image, leaveMounted bool) (info interface{}, err error) {
	return deployImage(config, img, leaveMounted, false)
}

// Deprecated: Use deployImage instead
func (e *Elemental) deployImage(img *v1.Image, leaveMounted, createDirStructure bool) (info interface{}, err error) {
	return deployImage(e.config, img, leaveMounted, createDirStructure)
}

// deployImage is the real function that does the actual work
// Set leaveMounted to leave the image mounted, otherwise it unmounts before returning
// Set createDirStructure to create the directory structure in the target, which creates the expected dirs
// for a running system. This is so we can reuse this method for creating random images, not only system ones
func deployImage(config *agentConfig.Config, img *v1.Image, leaveMounted, createDirStructure bool) (info interface{}, err error) {
	target := img.MountPoint
	if !img.Source.IsFile() {
		if img.FS != cnst.SquashFs {
			err = CreateFileSystemImage(config, img)
			if err != nil {
				return nil, err
			}

			err = MountImage(config, img, "rw")
			if err != nil {
				return nil, err
			}
		} else {
			target = utils.GetTempDir(config, "")
			err := fsutils.MkdirAll(config.Fs, target, cnst.DirPerm)
			if err != nil {
				return nil, err
			}
			defer config.Fs.RemoveAll(target) // nolint:errcheck
		}
	} else {
		target = img.File
	}
	info, err = DumpSource(config, target, img.Source)
	if err != nil {
		_ = UnmountImage(config, img)
		return nil, err
	}
	if !img.Source.IsFile() {
		if createDirStructure {
			err = utils.CreateDirStructure(config.Fs, target)
			if err != nil {
				return nil, err
			}
		}
		if img.FS == cnst.SquashFs {
			squashOptions := append(cnst.GetDefaultSquashfsOptions(), config.SquashFsCompressionConfig...)
			err = utils.CreateSquashFS(config.Runner, config.Logger, target, img.File, squashOptions)
			if err != nil {
				return nil, err
			}
		}
	} else if img.Label != "" && img.FS != cnst.SquashFs {
		out, err := config.Runner.Run("tune2fs", "-L", img.Label, img.File)
		if err != nil {
			config.Logger.Errorf("Failed to apply label %s to %s: %s", img.Label, img.File, string(out))
			_ = config.Fs.Remove(img.File)
			return nil, err
		}
	}
	if leaveMounted && img.Source.IsFile() {
		err = MountImage(config, img, "rw")
		if err != nil {
			return nil, err
		}
	}
	if !leaveMounted {
		err = UnmountImage(config, img)
		if err != nil {
			return nil, err
		}
	}
	return info, nil
}

// Deprecated: Use DumpSource instead
func (e *Elemental) DumpSource(target string, imgSrc *v1.ImageSource) (info interface{}, err error) { // nolint:gocyclo
	return DumpSource(e.config, target, imgSrc)
}

// DumpSource sets the image data according to the image source type
func DumpSource(config *agentConfig.Config, target string, imgSrc *v1.ImageSource) (info interface{}, err error) { // nolint:gocyclo
	config.Logger.Infof("Copying %s source to %s", imgSrc.Value(), target)

	if imgSrc.IsDocker() {
		if config.Cosign {
			config.Logger.Infof("Running cosign verification for %s", imgSrc.Value())
			out, err := utils.CosignVerify(
				config.Fs, config.Runner, imgSrc.Value(),
				config.CosignPubKey,
			)
			if err != nil {
				config.Logger.Errorf("Cosign verification failed: %s", out)
				return nil, err
			}
		}
		err = config.ImageExtractor.ExtractImage(imgSrc.Value(), target, config.Platform.String())
		if err != nil {
			return nil, err
		}
	} else if imgSrc.IsOCIFile() {
		// Extract OCI image from tar file
		config.Logger.Infof("Loading OCI image from tar file %s", imgSrc.Value())

		// Accounting for different image save conventions between tools, load the image from the tar file
		// First attempt: Try to load without specifying a tag
		img, err := tarball.ImageFromPath(imgSrc.Value(), nil)

		// Second attempt: If that fails, try with a oci-image:latest tag convention
		if err != nil {
			config.Logger.Infof("Trying to load with explicit oci-image:latest tag: %v", err)
			tag, tagErr := name.NewTag("oci-image:latest")
			if tagErr != nil {
				config.Logger.Errorf("Failed to create tag reference: %v", tagErr)
				return nil, fmt.Errorf("failed to create tag reference: %w", tagErr)
			}

			img, err = tarball.ImageFromPath(imgSrc.Value(), &tag)
			if err != nil {
				// Third attempt: Try to extract the tar file directly
				config.Logger.Infof("Trying to extract tar file directly: %v", err)

				// Create a temporary directory to extract the tar file
				tmpDir, err := fsutils.TempDir(config.Fs, "", "ocitar-extract")
				if err != nil {
					config.Logger.Errorf("Failed to create temporary directory: %v", err)
					return nil, fmt.Errorf("failed to create temporary directory: %w", err)
				}
				defer config.Fs.RemoveAll(tmpDir)

				// Extract the tar file to the temporary directory
				config.Logger.Infof("Extracting tar file to temporary directory: %s", tmpDir)
				//TODO: update to use native golang tar
				if out, err := config.Runner.Run("tar", "-xf", imgSrc.Value(), "-C", tmpDir); err != nil {
					config.Logger.Errorf("Failed to extract tar file: %v\n%s", err, string(out))
					return nil, fmt.Errorf("failed to extract tar file: %w", err)
				}

				// Copy the extracted contents to the target
				config.Logger.Infof("Copying extracted contents to target: %s", target)
				excludes := []string{}
				if err := utils.SyncData(config.Logger, config.Runner, config.Fs, tmpDir, target, excludes...); err != nil {
					config.Logger.Errorf("Failed to copy extracted contents: %v", err)
					return nil, fmt.Errorf("failed to copy extracted contents: %w", err)
				}

				// Successfully extracted and copied the contents
				return nil, nil
			}
		}

		if err != nil {
			config.Logger.Errorf("Failed to load image from tar file: %v", err)
			return nil, fmt.Errorf("failed to load image from tar file: %w", err)
		}

		// Extract the image contents to the target
		reader := mutate.Extract(img)
		_, err = archive.Apply(context.Background(), target, reader)
		if err != nil {
			config.Logger.Errorf("Failed to extract image contents: %v", err)
			return nil, fmt.Errorf("failed to extract image contents: %w", err)
		}
	} else if imgSrc.IsDir() {
		excludes := []string{"/mnt", "/proc", "/sys", "/dev", "/tmp", "/host", "/run"}
		err = utils.SyncData(config.Logger, config.Runner, config.Fs, imgSrc.Value(), target, excludes...)
		if err != nil {
			return nil, err
		}
	} else if imgSrc.IsFile() {
		err := fsutils.MkdirAll(config.Fs, filepath.Dir(target), cnst.DirPerm)
		if err != nil {
			return nil, err
		}
		err = utils.CopyFile(config.Fs, imgSrc.Value(), target)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unknown image source type")
	}
	config.Logger.Infof("Finished copying %s into %s", imgSrc.Value(), target)
	return info, nil
}

// Deprecated: Use CopyCloudConfig instead
func (e *Elemental) CopyCloudConfig(cloudInit []string) (err error) {
	return CopyCloudConfig(e.config, cloudInit)
}

// CopyCloudConfig will check if there is a cloud init in the config and store it on the target
func CopyCloudConfig(config *agentConfig.Config, cloudInit []string) (err error) {
	config.Logger.Infof("List of cloud inits to copy: %+v\n", cloudInit)
	for i, ci := range cloudInit {
		customConfig := filepath.Join(cnst.OEMDir, fmt.Sprintf("9%d_custom.yaml", i))
		config.Logger.Infof("Starting copying cloud config file %s to %s", ci, customConfig)
		err = utils.GetSource(config, ci, customConfig)
		if err != nil {
			return err
		}
		if err = config.Fs.Chmod(customConfig, cnst.ConfigPerm); err != nil {
			config.Logger.Debugf("Error on chmod %s: %s\n", customConfig, err.Error())
			return err
		}
		config.Logger.Infof("Finished copying cloud config file %s to %s", ci, customConfig)
	}
	return nil
}

// Deprecated: Use SelinuxRelabel instead
func (e *Elemental) SelinuxRelabel(rootDir string, raiseError bool) error {
	return SelinuxRelabel(e.config, rootDir, raiseError)
}

// SelinuxRelabel will relabel the system if it finds the binary and the context
func SelinuxRelabel(config *agentConfig.Config, rootDir string, raiseError bool) error {
	policyFile, err := utils.FindFileWithPrefix(config.Fs, filepath.Join(rootDir, cnst.SELinuxTargetedPolicyPath), "policy.")
	contextFile := filepath.Join(rootDir, cnst.SELinuxTargetedContextFile)
	contextExists, _ := fsutils.Exists(config.Fs, contextFile)

	if err == nil && contextExists && utils.CommandExists("setfiles") {
		var out []byte
		var err error
		if rootDir == "/" || rootDir == "" {
			out, err = config.Runner.Run("setfiles", "-c", policyFile, "-e", "/dev", "-e", "/proc", "-e", "/sys", "-F", contextFile, "/")
		} else {
			out, err = config.Runner.Run("setfiles", "-c", policyFile, "-F", "-r", rootDir, contextFile, rootDir)
		}
		config.Logger.Debugf("SELinux setfiles output: %s", string(out))
		if err != nil && raiseError {
			return err
		}
	} else {
		config.Logger.Debugf("No files relabelling as SELinux utilities are not found")
	}

	return nil
}

// Deprecated: Use CheckActiveDeployment instead
func (e *Elemental) CheckActiveDeployment(labels []string) bool {
	return CheckActiveDeployment(e.config, labels)
}

// CheckActiveDeployment returns true if at least one of the provided filesystem labels is found within the system
func CheckActiveDeployment(config *agentConfig.Config, labels []string) bool {
	config.Logger.Infof("Checking for active deployment")

	for _, label := range labels {
		found, _ := utils.GetDeviceByLabel(config, label, 1)
		if found != "" {
			config.Logger.Debug("there is already an active deployment in the system")
			return true
		}
	}
	return false
}

// Deprecated: Use GetIso instead
func (e *Elemental) GetIso(iso string) (tmpDir string, err error) {
	return GetIso(e.config, iso)
}

// GetIso will try to:
// download the iso into a temporary folder and mount the iso file as loop
// in cnst.DownloadedIsoMnt
func GetIso(config *agentConfig.Config, iso string) (tmpDir string, err error) {
	//TODO support ISO download in persistent storage?
	tmpDir, err = fsutils.TempDir(config.Fs, "", "elemental")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = config.Fs.RemoveAll(tmpDir)
		}
	}()

	isoMnt := filepath.Join(tmpDir, "iso")
	rootfsMnt := filepath.Join(tmpDir, "rootfs")

	tmpFile := filepath.Join(tmpDir, "cOs.iso")
	err = utils.GetSource(config, iso, tmpFile)
	if err != nil {
		return "", err
	}
	err = fsutils.MkdirAll(config.Fs, isoMnt, cnst.DirPerm)
	if err != nil {
		return "", err
	}
	config.Logger.Infof("Mounting iso %s into %s", tmpFile, isoMnt)
	err = config.Mounter.Mount(tmpFile, isoMnt, "auto", []string{"loop"})
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = config.Mounter.Unmount(isoMnt)
		}
	}()

	config.Logger.Infof("Mounting squashfs image from iso into %s", rootfsMnt)
	err = fsutils.MkdirAll(config.Fs, rootfsMnt, cnst.DirPerm)
	if err != nil {
		return "", err
	}
	err = config.Mounter.Mount(filepath.Join(isoMnt, cnst.IsoRootFile), rootfsMnt, "auto", []string{})
	return tmpDir, err
}

// Deprecated: Use UpdateSourcesFormDownloadedISO instead
func (e Elemental) UpdateSourcesFormDownloadedISO(workDir string, activeImg *v1.Image, recoveryImg *v1.Image) error {
	return UpdateSourcesFormDownloadedISO(e.config, workDir, activeImg, recoveryImg)
}

// UpdateSourcesFormDownloadedISO checks a downaloaded and mounted ISO in workDir and updates the active and recovery image
// descriptions to use the squashed rootfs from the downloaded ISO.
func UpdateSourcesFormDownloadedISO(config *agentConfig.Config, workDir string, activeImg *v1.Image, recoveryImg *v1.Image) error {
	rootfsMnt := filepath.Join(workDir, "rootfs")
	isoMnt := filepath.Join(workDir, "iso")

	if activeImg != nil {
		activeImg.Source = v1.NewDirSrc(rootfsMnt)
	}
	if recoveryImg != nil {
		squashedImgSource := filepath.Join(isoMnt, cnst.RecoverySquashFile)
		if exists, _ := fsutils.Exists(config.Fs, squashedImgSource); exists {
			recoveryImg.Source = v1.NewFileSrc(squashedImgSource)
			recoveryImg.FS = cnst.SquashFs
		} else if activeImg != nil {
			recoveryImg.Source = v1.NewFileSrc(activeImg.File)
			recoveryImg.FS = cnst.LinuxImgFs
			// Only update label if unset, it could happen if the host is running form another ISO.
			if recoveryImg.Label == "" {
				recoveryImg.Label = cnst.SystemLabel
			}
		} else {
			return fmt.Errorf("can't set recovery image from ISO, source image is missing")
		}
	}
	return nil
}

// Deprecated: Use SetDefaultGrubEntry instead
func (e Elemental) SetDefaultGrubEntry(partMountPoint string, imgMountPoint string, defaultEntry string) error {
	return SetDefaultGrubEntry(e.config, partMountPoint, imgMountPoint, defaultEntry)
}

// SetDefaultGrubEntry Sets the default_menu_entry value in Config.GrubOEMEnv file at in
// State partition mountpoint. If there is not a custom value in the kairos-release file, we do nothing
// As the grub config already has a sane default
func SetDefaultGrubEntry(config *agentConfig.Config, partMountPoint string, imgMountPoint string, defaultEntry string) error {
	if defaultEntry == "" {
		var osRelease map[string]string
		osRelease, err := utils.LoadEnvFile(config.Fs, filepath.Join(imgMountPoint, "etc", "kairos-release"))
		if err != nil {
			// Fallback to os-release
			osRelease, err = utils.LoadEnvFile(config.Fs, filepath.Join(imgMountPoint, "etc", "os-release"))
			config.Logger.Warnf("Could not load os-release file: %v", err)
			return nil
		}
		defaultEntry = osRelease["GRUB_ENTRY_NAME"]
		// If its still empty then do nothing
		if defaultEntry == "" {
			return nil
		}
	}
	config.Logger.Infof("Setting default grub entry to %s", defaultEntry)
	return utils.SetPersistentVariables(
		filepath.Join(partMountPoint, cnst.GrubOEMEnv),
		map[string]string{"default_menu_entry": defaultEntry},
		config,
	)
}

// Deprecated: Use FindKernelInitrd instead
func (e Elemental) FindKernelInitrd(rootDir string) (kernel string, initrd string, err error) {
	return FindKernelInitrd(e.config, rootDir)
}

// FindKernelInitrd finds for kernel and intird files inside the /boot directory of a given
// root tree path. It assumes kernel and initrd files match certain file name prefixes.
func FindKernelInitrd(config *agentConfig.Config, rootDir string) (kernel string, initrd string, err error) {
	kernelNames := []string{"uImage", "Image", "zImage", "vmlinuz", "image"}
	initrdNames := []string{"initrd", "initramfs"}
	kernel, err = utils.FindFileWithPrefix(config.Fs, filepath.Join(rootDir, "boot"), kernelNames...)
	if err != nil {
		config.Logger.Errorf("No Kernel file found")
		return "", "", err
	}
	initrd, err = utils.FindFileWithPrefix(config.Fs, filepath.Join(rootDir, "boot"), initrdNames...)
	if err != nil {
		config.Logger.Errorf("No initrd file found")
		return "", "", err
	}
	return kernel, initrd, nil
}

// Deprecated: Use DeactivateDevices instead
func (e Elemental) DeactivateDevices() error {
	return DeactivateDevices(e.config)
}

// DeactivateDevices deactivates unmounted the block devices present within the system.
// Useful to deactivate LVM volumes, if any, related to the target device.
func DeactivateDevices(config *agentConfig.Config) error {
	out, err := config.Runner.Run(
		"blkdeactivate", "--lvmoptions", "retry,wholevg",
		"--dmoptions", "force,retry", "--errors",
	)
	config.Logger.Debugf("blkdeactivate command output: %s", string(out))
	return err
}
