package phonehome

import (
	"os"
	"strings"
)

// Boot-state values reported to the fleet server, aligned with the day-2
// lifecycle vocabulary agreed on kairos-io/kairos#4253:
//
//	active | passive | recovery | autoreset | livecd
//
// The server accepts unknown values too, but the agent maps to this set.
const (
	bootStateActive    = "active"
	bootStatePassive   = "passive"
	bootStateRecovery  = "recovery"
	bootStateAutoReset = "autoreset"
	bootStateLiveCD    = "livecd"
)

// classifyBootState derives the boot state from the kernel command line. It
// mirrors kairos-sdk/state.DetectBootWithVFS (which matches the COS_* rootfs
// markers on /proc/cmdline) and additionally recognises the automatic
// state-reset boot, which the SDK reports as Recovery. The statereset GRUB entry
// boots the recovery system with `kairos.reset` on the command line, so that
// marker (which also implies a recovery cmdline) is checked first.
func classifyBootState(cmdline string) string {
	switch {
	case strings.Contains(cmdline, "kairos.reset"):
		return bootStateAutoReset
	case strings.Contains(cmdline, "COS_PASSIVE"):
		return bootStatePassive
	case strings.Contains(cmdline, "COS_RECOVERY"),
		strings.Contains(cmdline, "COS_SYSTEM"),
		strings.Contains(cmdline, "recovery-mode"):
		return bootStateRecovery
	case strings.Contains(cmdline, "live:LABEL"),
		strings.Contains(cmdline, "live:CDLABEL"),
		strings.Contains(cmdline, "netboot"):
		return bootStateLiveCD
	default:
		// COS_ACTIVE or anything else: a normally running node reports active.
		return bootStateActive
	}
}

// detectBootState reads /proc/cmdline and classifies the node's boot state.
// Best-effort: if the command line cannot be read it returns "active".
func detectBootState() string {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return bootStateActive
	}
	return classifyBootState(string(cmdline))
}
