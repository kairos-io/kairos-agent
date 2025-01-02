package loop

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/types/v1"
)

var (
	instance *LoopManager
	once     sync.Once
)

type LoopManager struct {
	mu  sync.Mutex
	cfg *config.Config
}

// GetLoopManager returns a singleton instance of LoopManager
// So we can safely call it from different places and it will work properly with the mutex locking
func GetLoopManager(cfg *config.Config) *LoopManager {
	once.Do(func() {
		instance = &LoopManager{
			cfg: cfg,
		}
	})
	return instance
}

func (lm *LoopManager) errnoIsErr(err error) error {
	if err != nil && err.(syscall.Errno) != 0 {
		return err
	}
	return nil
}

func (lm *LoopManager) Loop(img *v1.Image) (loopDevice string, err error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	log := lm.cfg.Logger
	log.Debugf("Opening loop control device")
	fd, err := lm.cfg.Fs.OpenFile("/dev/loop-control", os.O_RDONLY, 0o644)
	if err != nil {
		log.Error("failed to open /dev/loop-control")
		return loopDevice, err
	}
	defer fd.Close()

	log.Debugf("Getting free loop device")
	loopInt, _, err := lm.cfg.Syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CTL_GET_FREE, 0)
	if lm.errnoIsErr(err) != nil {
		log.Error("failed to get loop device")
		return loopDevice, err
	}

	loopDevice = fmt.Sprintf("/dev/loop%d", loopInt)
	log.Logger.Debug().Str("device", loopDevice).Msg("Opening loop device")
	loopFile, err := lm.cfg.Fs.OpenFile(loopDevice, os.O_RDWR, 0)
	if err != nil {
		log.Error("failed to open loop device")
		return loopDevice, err
	}
	defer loopFile.Close()

	log.Logger.Debug().Str("image", img.File).Msg("Opening img file")
	imageFile, err := lm.cfg.Fs.OpenFile(img.File, os.O_RDWR, os.ModePerm)
	if err != nil {
		log.Error("failed to open image file")
		return loopDevice, err
	}
	defer imageFile.Close()

	log.Debugf("Setting loop device")
	_, _, err = lm.cfg.Syscall.Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_FD,
		imageFile.Fd(),
	)
	if lm.errnoIsErr(err) != nil {
		log.Error("failed to set loop device")
		return loopDevice, err
	}

	status := &unix.LoopInfo64{
		Flags: unix.LO_FLAGS_PARTSCAN,
	}
	status.Flags &= ^uint32(unix.LO_FLAGS_READ_ONLY)

	log.Debugf("Setting loop flags")
	_, _, err = lm.cfg.Syscall.Syscall(
		syscall.SYS_IOCTL,
		loopFile.Fd(),
		unix.LOOP_SET_STATUS64,
		uintptr(unsafe.Pointer(status)),
	)
	if lm.errnoIsErr(err) != nil {
		log.Error("failed to set loop device status")
		return loopDevice, err
	}

	return loopDevice, nil
}

func (lm *LoopManager) Unloop(loopDevice string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	log := lm.cfg.Logger
	log.Logger.Debug().Str("device", loopDevice).Msg("Opening loop device")
	fd, err := lm.cfg.Fs.OpenFile(loopDevice, os.O_RDONLY, 0o644)
	if err != nil {
		log.Error("failed to set open loop device")
		return err
	}
	defer fd.Close()

	log.Debugf("Clearing loop device")
	_, _, err = lm.cfg.Syscall.Syscall(syscall.SYS_IOCTL, fd.Fd(), unix.LOOP_CLR_FD, 0)
	if lm.errnoIsErr(err) != nil {
		log.Error("failed to set loop device status")
		return err
	}

	return nil
}
