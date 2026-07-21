package state

import (
	"time"

	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
)

// BackupSuffix is appended to a corrupt state file before writing a fresh one.
const BackupSuffix = ".bak"

// Clock returns the current time. Kept as a function value so tests can inject
// a deterministic clock.
type Clock func() time.Time

// loadOrRecover loads the state from mountPoint. If the file exists but is
// unparseable it is renamed to <path>.bak (overwriting any prior backup) and a
// fresh Default() is returned. IO errors that are not parse failures are
// returned as-is.
func loadOrRecover(fs sdkFs.KairosFS, path string) (*KairosState, error) {
	s, err := Load(fs, path)
	if err == nil {
		return s, nil
	}
	// Parse error path: back up and start fresh. If the file is not present
	// at all we would not be here — Load returns Default() in that case.
	if _, statErr := fs.Stat(path); statErr == nil {
		_ = fs.Rename(path, path+BackupSuffix)
		return Default(), nil
	}
	return nil, err
}

func stamp(now Clock) string {
	return now().UTC().Format(time.RFC3339)
}

// RecordInstall writes only the last-install and last-install-source fields.
func RecordInstall(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastInstall = stamp(now)
	s.LastInstallSource = source
	return s.Save(fs, path)
}

// RecordActiveUpgrade shifts the current active values into the passive slots
// and writes the new active values.
func RecordActiveUpgrade(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastPassiveUpgrade = s.LastActiveUpgrade
	s.LastPassiveSource = s.LastActiveSource
	s.LastActiveUpgrade = stamp(now)
	s.LastActiveSource = source
	return s.Save(fs, path)
}

// RecordRecoveryUpgrade writes only the last-recovery-* fields.
func RecordRecoveryUpgrade(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastRecoveryUpgrade = stamp(now)
	s.LastRecoverySource = source
	return s.Save(fs, path)
}

// RecordReset writes the last-reset-* fields and clears the passive fields
// back to defaults (reset wipes the passive image so history for it no longer
// applies).
func RecordReset(fs sdkFs.KairosFS, mountPoint, source string, now Clock) error {
	path := Path(mountPoint)
	s, err := loadOrRecover(fs, path)
	if err != nil {
		return err
	}
	s.LastReset = stamp(now)
	s.LastResetSource = source
	s.LastPassiveUpgrade = NeverValue
	s.LastPassiveSource = ""
	return s.Save(fs, path)
}
