package server

import (
	"fmt"
	"strings"
	"testing"
)

func TestRetainedViewportAnchorsAfterHistoryRingFills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("history", t.TempDir(), "/bin/sh", 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	p.mu.Lock()
	var output strings.Builder
	for i := 0; i < retainedHistoryLines+20; i++ {
		fmt.Fprintf(&output, "ROW%05d\r\n", i)
	}
	p.trackHistory(func() { _, _ = p.term.WriteString(output.String()) })
	p.revision++
	p.mu.Unlock()
	offsets, histories := map[int]int{}, map[int]int{}
	_, revision := s.snapshot(offsets, histories, 0, true)
	offsets[1] = 5
	before, revision := s.snapshot(offsets, histories, revision, true)
	old := before.Panes[0].Terminal
	if old.History != retainedHistoryLines {
		t.Fatal("history ring was not filled")
	}
	p.mu.Lock()
	p.trackHistory(func() { _, _ = p.term.WriteString("NEW1\r\nNEW2\r\nNEW3\r\n") })
	p.revision++
	p.mu.Unlock()
	after, _ := s.snapshot(offsets, histories, revision, false)
	now := after.Panes[0].Terminal
	if now.Base != old.Base || frameText(now) != frameText(old) {
		t.Fatalf("retained viewport drifted: base %d->%d", old.Base, now.Base)
	}
	if now.HistoryStart <= old.HistoryStart || now.Offset <= old.Offset {
		t.Fatal("eviction and cumulative line movement were not recorded")
	}
	p.mu.Lock()
	p.trackHistory(func() { _, _ = p.term.WriteString("\x1b[3J") })
	p.revision++
	p.mu.Unlock()
	reset, _ := s.snapshot(offsets, histories, 0, true)
	if reset.Panes[0].Terminal.Epoch == now.Epoch {
		t.Fatal("history reset reused screen identity")
	}
}
