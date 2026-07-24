package phonehome

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("classifyBootState",
	func(cmdline, want string) { Expect(classifyBootState(cmdline)).To(Equal(want)) },
	Entry("active boot", "BOOT_IMAGE=/boot/vmlinuz root=LABEL=COS_ACTIVE ro console=tty1", bootStateActive),
	Entry("passive boot (active image broken)", "root=LABEL=COS_PASSIVE ro", bootStatePassive),
	Entry("recovery boot", "root=LABEL=COS_RECOVERY ro console=tty1", bootStateRecovery),
	Entry("recovery via recovery-mode", "root=/dev/sda2 recovery-mode ro", bootStateRecovery),
	// The statereset entry boots the recovery system (COS_RECOVERY present) AND
	// carries kairos.reset; kairos.reset must win => autoreset, not recovery.
	Entry("autoreset (statereset boot)", "root=LABEL=COS_RECOVERY vga=795 nomodeset kairos.reset", bootStateAutoReset),
	Entry("livecd (label)", "root=live:LABEL=COS_LIVE ro", bootStateLiveCD),
	Entry("livecd (netboot)", "root=/dev/nfs netboot ip=dhcp", bootStateLiveCD),
	Entry("no marker defaults to active", "root=/dev/vda1 ro quiet", bootStateActive),
)
