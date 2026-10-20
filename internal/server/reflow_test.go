package server

import (
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestPaneReflowsOnResizeAndFramesMarkWrappedRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("reflow", t.TempDir(), "/bin/sh", 10, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	p.mu.Lock()
	p.trackHistory(func() { _, _ = p.term.WriteString("\x1b[2J\x1b[Habcdefghijklmnop") })
	p.mu.Unlock()
	before, _ := p.frame(0)
	if len(before.Wrapped) == 0 || !before.Wrapped[0] || before.Wrapped[1] {
		t.Fatalf("wrap flags %v", before.Wrapped)
	}
	if err := p.input(protocol.Request{Op: "resize", Width: 20, Height: 4}); err != nil {
		t.Fatal(err)
	}
	after, _ := p.frame(0)
	if !strings.Contains(frameText(after), "abcdefghijklmnop") {
		t.Fatalf("not rewrapped at 20 columns:\n%s", frameText(after))
	}
	// History and screen rows moved, so old row IDs mean nothing now.
	if after.Epoch == before.Epoch {
		t.Fatal("reflow kept the coordinate epoch")
	}
	// A height-only change moves rows in order and keeps the epoch.
	if err := p.input(protocol.Request{Op: "resize", Width: 20, Height: 3}); err != nil {
		t.Fatal(err)
	}
	if shorter, _ := p.frame(0); shorter.Epoch != after.Epoch {
		t.Fatal("a height change started a new epoch")
	}
}
