package server

import (
	"fmt"
	"strings"
	"testing"
)

func TestViewportAnchorStaysFixedDuringConcurrentOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("atomic-history", t.TempDir(), "/bin/sh", 30, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	p.mu.Lock()
	var seed strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&seed, "SEED%06d\r\n", i)
	}
	p.trackHistory(func() { _, _ = p.term.WriteString(seed.String()) })
	p.revision++
	p.mu.Unlock()
	offsets, lastTotals := map[int]int{}, map[int]int{}
	_, revision := s.snapshot(offsets, lastTotals, 0, true)
	offsets[1] = 50
	first, revision := s.snapshot(offsets, lastTotals, revision, true)
	anchor, contents := first.Panes[0].Terminal.Base, frameText(first.Panes[0].Terminal)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			p.mu.Lock()
			p.trackHistory(func() { _, _ = p.term.WriteString(fmt.Sprintf("LIVE%06d\r\n", i)) })
			p.revision++
			p.mu.Unlock()
		}
	}()
	for i := 0; i < 3000; i++ {
		f, rev := s.snapshot(offsets, lastTotals, revision, true)
		revision = rev
		if f.Panes[0].Terminal.Base != anchor || frameText(f.Panes[0].Terminal) != contents {
			<-done
			t.Fatal("concurrent output changed a still-retained scrollback anchor")
		}
	}
	<-done
}
