package boards

import (
	"fmt"
	"github.com/joho/godotenv"
	"github.com/kairos-io/kairos-agent/v2/pkg/partitioner"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/partitions"
	"github.com/sanity-io/litter"
	"os"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
)

// IsAnAndroidBoard returns true if the system is an Android board
// based on checking if there is a build.prop file
func IsAnAndroidBoard() bool {
	// Check if we are running on an Android board
	_, err := os.Stat("/build.prop")
	if err == nil {
		return true
	}
	return false
}

// GetAndroidBoardModel returns the board model if the system is an Android board
func GetAndroidBoardModel() string {
	// Check if we are running on an Android board
	if IsAnAndroidBoard() {
		buildProp, err := godotenv.Read("/build.prop")
		if err != nil {
			return ""
		}
		switch buildProp["ro.product.board"] {
		case cnst.QCS6490:
			return cnst.QCS6490
		default:
			return ""
		}

	}

	return ""
}

// GetPartitions returns the system and passive partitions
func GetPartitions() (err error, active *v1.Partition, passive *v1.Partition) {
	part, _ := partitions.GetAllPartitions()
	for _, p := range part {
		switch p.Label {
		case "system":
			active = p
		case "passive":
			passive = p
		}
	}

	if active == nil || passive == nil {
		return fmt.Errorf("could not find system or passive partition"), active, passive
	}
	return nil, active, passive
}

// SetPassiveActive sets the passive partition as active and the active partition as passive
// Only valid for QCS6490 boards
func SetPassiveActive(runner v1.Runner, logger v1.Logger) (error error, out string) {
	err, active, _ := GetPartitions()
	if err != nil {
		return err, ""
	}
	// This is to get the partition number itself as sgdisk needs the number directly not the device
	parted := partitioner.NewPartedCall(active.Disk, runner)
	prnt, _ := parted.Print()
	parts := parted.GetPartitions(prnt)
	for _, p := range parts {
		if p.PLabel == cnst.QCS6490_passive_label {
			logger.Debugf("Found partition %d as passive", litter.Sdump(p))
			logger.Info("Setting passive partition as active")
			// Change passive partition label to system
			run, err := runner.Run("sgdisk", active.Disk, "-c", fmt.Sprintf("%d:%s", p.Number, cnst.QCS6490_system_label))
			if err != nil {
				return err, string(run)
			}
		}
		if p.PLabel == cnst.QCS6490_system_label {
			logger.Debugf("Found partition %d as active", litter.Sdump(p))
			logger.Info("Setting active partition as passive")
			// Change active partition label to passive
			run, err := runner.Run("sgdisk", active.Disk, "-c", fmt.Sprintf("%d:%s", p.Number, cnst.QCS6490_passive_label))
			if err != nil {
				return err, string(run)
			}
		}

	}

	_, _ = runner.Run("sync")
	return nil, ""
}