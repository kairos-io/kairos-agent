package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsNVMeControllerPath(t *testing.T) {
	hidden := []string{"nvme1c1n1", "nvme0c0n1", "nvme10c2n3", "nvme1c1n1p2"}
	for _, n := range hidden {
		if !isNVMeControllerPath(n) {
			t.Errorf("expected %q to be detected as NVMe controller-path device", n)
		}
	}
	real := []string{"nvme1n1", "nvme0n1", "nvme1n1p2", "sda", "dm-0", "nvme1n1c1" /* not a controller path */}
	for _, n := range real {
		if isNVMeControllerPath(n) {
			t.Errorf("expected %q NOT to be detected as NVMe controller-path device", n)
		}
	}
}

func TestDeviceIsHidden(t *testing.T) {
	dir := t.TempDir()
	old := sysBlockPath
	sysBlockPath = dir
	defer func() { sysBlockPath = old }()

	writeHidden := func(name, val string) {
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "hidden"), []byte(val), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// NVMe per-controller path device: hidden, must be reported as hidden.
	writeHidden("nvme1c1n1", "1\n")
	// Real namespace: not hidden.
	writeHidden("nvme1n1", "0\n")
	// A disk with no hidden attribute at all must default to not hidden.
	if err := os.MkdirAll(filepath.Join(dir, "sda"), 0755); err != nil {
		t.Fatal(err)
	}

	if !deviceIsHidden("nvme1c1n1") {
		t.Error("expected nvme1c1n1 (hidden==1) to be reported hidden")
	}
	if deviceIsHidden("nvme1n1") {
		t.Error("expected nvme1n1 (hidden==0) to be reported not hidden")
	}
	if deviceIsHidden("sda") {
		t.Error("expected sda (no hidden attr) to be reported not hidden")
	}
}

func makeDiskPage(n int) *diskSelectionPage {
	disks := make([]diskStruct, n)
	for i := 0; i < n; i++ {
		disks[i] = diskStruct{id: i, name: "/dev/sd" + string(rune('a'+i)), size: "10.00 GiB"}
	}
	return &diskSelectionPage{disks: disks}
}

func pressDown(p *diskSelectionPage) {
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
}

func pressUp(p *diskSelectionPage) {
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
}

// cursorVisible reports whether the cursor index falls inside the scroll window.
func cursorVisible(p *diskSelectionPage) bool {
	vc := p.visibleCount()
	return p.cursor >= p.offset && p.cursor < p.offset+vc
}

func TestDiskScrollCursorStaysVisible(t *testing.T) {
	// Small terminal so the disk list overflows: height 24 -> ~10 visible rows.
	mainModel = Model{width: 80, height: 24}
	p := makeDiskPage(30)

	if len(p.disks) <= p.visibleCount() {
		t.Fatalf("test precondition: list (%d) should overflow visible window (%d)", len(p.disks), p.visibleCount())
	}

	// Walk all the way down. Cursor must remain inside the window at every step.
	for i := 0; i < len(p.disks)-1; i++ {
		pressDown(p)
		if !cursorVisible(p) {
			t.Fatalf("cursor %d not visible after %d downs: offset=%d visible=%d", p.cursor, i+1, p.offset, p.visibleCount())
		}
	}
	if p.cursor != len(p.disks)-1 {
		t.Fatalf("expected cursor at last disk %d, got %d", len(p.disks)-1, p.cursor)
	}

	// Walk back up. Same invariant.
	for i := 0; i < len(p.disks)-1; i++ {
		pressUp(p)
		if !cursorVisible(p) {
			t.Fatalf("cursor %d not visible after going up: offset=%d visible=%d", p.cursor, p.offset, p.visibleCount())
		}
	}
	if p.cursor != 0 || p.offset != 0 {
		t.Fatalf("expected cursor/offset back at 0, got cursor=%d offset=%d", p.cursor, p.offset)
	}
}

func TestDiskScrollNoOverflowSmallList(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makeDiskPage(3)

	// No scroll indicators when everything fits.
	out := p.View()
	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Fatalf("did not expect scroll indicators for a short list:\n%s", out)
	}
	if p.offset != 0 {
		t.Fatalf("offset should stay 0 for short list, got %d", p.offset)
	}
}

func TestDiskScrollIndicatorsAndViewBounds(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makeDiskPage(30)

	// At the top: only the "below" indicator.
	top := p.View()
	if strings.Contains(top, "more above") {
		t.Fatalf("unexpected 'more above' at top of list:\n%s", top)
	}
	if !strings.Contains(top, "more below") {
		t.Fatalf("expected 'more below' at top of long list:\n%s", top)
	}

	// Move into the middle: both indicators present.
	for i := 0; i < 15; i++ {
		pressDown(p)
	}
	mid := p.View()
	if !strings.Contains(mid, "more above") || !strings.Contains(mid, "more below") {
		t.Fatalf("expected both indicators in the middle:\n%s", mid)
	}

	// The rendered content must never exceed the Model.View slice budget
	// (height-10). strings.Count counts trailing-newline-terminated rows.
	_, h := effectiveSize(mainModel.width, mainModel.height)
	budget := h - 10
	if got := strings.Count(mid, "\n"); got > budget {
		t.Fatalf("rendered %d content lines, exceeds budget %d", got, budget)
	}
}
