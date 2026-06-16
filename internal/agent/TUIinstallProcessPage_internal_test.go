package agent

import (
	"bytes"
	"errors"
	"testing"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

// TestProcessLogLinesDoesNotFlagErrorState verifies that an error/fatal log
// line emitted mid-install no longer flips the page into a terminal error
// state. The install may log a recoverable error and carry on; the real
// outcome comes from RunInstall's return value, not log scraping.
func TestProcessLogLinesDoesNotFlagErrorState(t *testing.T) {
	p := newInstallProcessPage()
	var logBuf bytes.Buffer
	log := sdkLogger.NewBufferLogger(&logBuf)

	line := []byte(`{"level":"error","message":"Disk /dev/nvme1c1n1 does not exist"}` + "\n")
	p.processLogLines(line, 0, &log)

	if p.errorMsg != "" {
		t.Fatalf("error-level log must not set errorMsg, got %q", p.errorMsg)
	}
}

// TestApplyFinalState verifies the terminal state is decided by the RunInstall
// result published in installErr, not by anything else.
func TestApplyFinalState(t *testing.T) {
	pf := newInstallProcessPage()
	pf.installErr = errors.New("install blew up")
	pf.applyFinalState()
	if pf.errorMsg != "install blew up" {
		t.Fatalf("expected errorMsg from installErr, got %q", pf.errorMsg)
	}

	ps := newInstallProcessPage()
	ps.applyFinalState()
	if ps.errorMsg != "" {
		t.Fatalf("expected no errorMsg on success, got %q", ps.errorMsg)
	}
	if ps.progress != len(ps.steps)-1 {
		t.Fatalf("expected progress at last step %d, got %d", len(ps.steps)-1, ps.progress)
	}
}

// TestUpdateDoneBranch exercises the done select branch (output still open,
// done closed).
func TestUpdateDoneBranch(t *testing.T) {
	p := newInstallProcessPage()
	p.installErr = errors.New("done-path failure")
	close(p.done)
	if _, _ = p.Update(CheckInstallerMsg{}); p.errorMsg != "done-path failure" {
		t.Fatalf("done branch did not apply terminal state, got %q", p.errorMsg)
	}
}

// TestUpdateClosedOutputBranch exercises the closed-output select branch, which
// must also apply the terminal state. Before the fix this returned without
// touching progress/error, freezing the UI at 0%. output is closed while done
// stays open, so the closed-output case is the only ready one (deterministic).
func TestUpdateClosedOutputBranch(t *testing.T) {
	p := newInstallProcessPage()
	p.installErr = errors.New("closed-output failure")
	close(p.output)
	if _, _ = p.Update(CheckInstallerMsg{}); p.errorMsg != "closed-output failure" {
		t.Fatalf("closed-output branch did not apply terminal state, got %q", p.errorMsg)
	}

	// Success variant: nil installErr -> progress reaches the last step.
	ps := newInstallProcessPage()
	close(ps.output)
	if _, _ = ps.Update(CheckInstallerMsg{}); ps.progress != len(ps.steps)-1 {
		t.Fatalf("closed-output success did not reach last step, got %d", ps.progress)
	}
}
