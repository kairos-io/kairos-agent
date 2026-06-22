package agent

import (
	"strings"
	"testing"
)

func TestPhoneHomeServiceUnit(t *testing.T) {
	unit := phoneHomeServiceUnit("/usr/bin/kairos-agent")

	if !strings.Contains(unit, "ExecStart=/usr/bin/kairos-agent phone-home") {
		t.Fatalf("ExecStart should use the provided binary path, got:\n%s", unit)
	}
	if strings.Contains(unit, "/usr/sbin/kairos-agent") {
		t.Fatalf("unit should not hardcode /usr/sbin/kairos-agent, got:\n%s", unit)
	}
}
