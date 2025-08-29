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

package deploy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"

	"github.com/containerd/containerd/archive"
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
	"github.com/kairos-io/kairos-sdk/types"
)

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

// UnmountPartition unmounts the given partition or does nothing if not mounted
func UnmountPartition(config *agentConfig.Config, part *types.Partition) error {
	if mnt, _ := utils.IsMounted(config, part); !mnt {
		config.Logger.Debugf("Not unmounting partition, %s doesn't look like mountpoint", part.MountPoint)
		return nil
	}
	config.Logger.Debugf("Unmounting partition %s", part.FilesystemLabel)
	return config.Mounter.Unmount(part.MountPoint)
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

// DeployImage will deploy the given image into the target. This method
// creates the filesystem image file, mounts it and unmounts it as needed.
// Creates the default system dirs by default (/sys,/proc,/dev, etc...)
func DeployImage(config *agentConfig.Config, img *v1.Image, leaveMounted bool) (info interface{}, err error) {
	return deployImage(config, img, leaveMounted, true)
}

// DeployImageNodirs will deploy the given image into the target. This method
// creates the filesystem image file, mounts it and unmounts it as needed.
// Does not create the default system dirs so it can be used to create generic images from any source
func DeployImageNodirs(config *agentConfig.Config, img *v1.Image, leaveMounted bool) (info interface{}, err error) {
	return deployImage(config, img, leaveMounted, false)
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
