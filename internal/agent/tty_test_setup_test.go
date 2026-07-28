package agent

// This init runs when the agent package's *_test.go files compile into the
// test binary. It replaces startGettyTTY1 with a no-op so no test can ever
// reach machine.Getty(1).Start(). On a systemd developer host that call runs
// `systemctl start getty@tty1.service`, which switches away from the running
// TTY and disrupts the developer's session — regardless of whether the test
// meant to exercise the interactive path.
func init() {
	startGettyTTY1 = func() {}
}
