package server

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func windowSession(t *testing.T) *session {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("windows", t.TempDir(), "/bin/sh", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.stop)
	return s
}

func mustInput(t *testing.T, s *session, r protocol.Request) {
	t.Helper()
	if err := s.input(r); err != nil {
		t.Fatalf("%s: %v", r.Op, err)
	}
}

func windowPaneIDs(f *protocol.Frame) [][]int {
	var result [][]int
	for _, w := range f.Windows {
		result = append(result, w.Panes)
	}
	return result
}

func activeWindow(f *protocol.Frame) int {
	for i, w := range f.Windows {
		if w.Active {
			return i + 1
		}
	}
	return 0
}

func awaitPaneGone(t *testing.T, s *session, id int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		gone := s.panes[id] == nil
		s.mu.Unlock()
		if gone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %d never left", id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWindowsKeepTheirOwnLayoutsAndSwitch(t *testing.T) {
	s := windowSession(t)
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // window 1: panes 1, 2
	mustInput(t, s, protocol.Request{Op: "new-window", NewName: "logs"})    // window 2: pane 3
	mustInput(t, s, protocol.Request{Op: "split", Direction: "vertical"})   // window 2: panes 3, 4

	f := snapshot(s)
	if got := windowPaneIDs(f); !reflect.DeepEqual(got, [][]int{{1, 2}, {3, 4}}) || activeWindow(f) != 2 {
		t.Fatalf("windows %v, active %d", got, activeWindow(f))
	}
	if f.Windows[1].Name != "logs" || f.Focused != 4 || len(f.Panes) != 2 || f.Panes[0].ID != 3 {
		t.Fatalf("active window frame: name %q focused %d panes %+v", f.Windows[1].Name, f.Focused, f.Panes)
	}
	// Every window is laid out at the session size; switching resizes nothing.
	s.mu.Lock()
	for _, w := range s.windows {
		for _, pos := range w.placements(s.width, s.height) {
			p := s.panes[pos.id]
			p.mu.Lock()
			width, height := p.term.Width(), p.term.Height()
			p.mu.Unlock()
			if width != pos.w || height != pos.h {
				t.Errorf("pane %d is %dx%d, laid out %dx%d", pos.id, width, height, pos.w, pos.h)
			}
		}
	}
	s.mu.Unlock()

	for _, tc := range []struct {
		r      protocol.Request
		active int
		focus  int
	}{
		{protocol.Request{Op: "select-window", Window: 1}, 1, 2},
		{protocol.Request{Op: "select-window", Direction: "last"}, 2, 4},
		{protocol.Request{Op: "select-window", Direction: "next"}, 1, 2}, // wraps
		{protocol.Request{Op: "select-window", Direction: "previous"}, 2, 4},
		// A pane in a background window brings its window forward.
		{protocol.Request{Op: "select-pane", PaneID: 1}, 1, 1},
	} {
		mustInput(t, s, tc.r)
		if f := snapshot(s); activeWindow(f) != tc.active || f.Focused != tc.focus {
			t.Fatalf("after %+v: window %d focus %d, want %d and %d", tc.r, activeWindow(f), f.Focused, tc.active, tc.focus)
		}
	}
	for _, r := range []protocol.Request{{Op: "select-window", Window: 3}, {Op: "select-window", Direction: "sideways"}, {Op: "rename-window", NewName: "a\x1bb"}, {Op: "rename-window", Window: 9, NewName: "x"}} {
		if err := s.input(r); err == nil {
			t.Fatalf("%+v accepted", r)
		}
	}
	mustInput(t, s, protocol.Request{Op: "rename-window", NewName: "edit"})
	if f := snapshot(s); f.Windows[0].Name != "edit" || f.Windows[1].Name != "logs" {
		t.Fatalf("rename: %+v", f.Windows)
	}
}

func TestClosingWindowsRenumbersAndEndsSessionWithTheLast(t *testing.T) {
	s := windowSession(t)
	mustInput(t, s, protocol.Request{Op: "new-window"}) // 2: pane 2
	mustInput(t, s, protocol.Request{Op: "new-window"}) // 3: pane 3
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 1})
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 2})
	// Closing the active window returns to the last one, like tmux.
	mustInput(t, s, protocol.Request{Op: "close-window"})
	awaitPaneGone(t, s, 2)
	f := snapshot(s)
	if got := windowPaneIDs(f); !reflect.DeepEqual(got, [][]int{{1}, {3}}) || activeWindow(f) != 1 {
		t.Fatalf("after closing window 2: %v active %d", got, activeWindow(f))
	}
	// A shell exiting in a background window removes that window only.
	mustInput(t, s, protocol.Request{Op: "close-pane", PaneID: 3})
	awaitPaneGone(t, s, 3)
	if f := snapshot(s); !reflect.DeepEqual(windowPaneIDs(f), [][]int{{1}}) || f.Focused != 1 {
		t.Fatalf("after closing background window: %v focus %d", windowPaneIDs(f), f.Focused)
	}
	if err := s.input(protocol.Request{Op: "select-window", Direction: "last"}); err == nil {
		t.Fatal("last window survived its window closing")
	}
	mustInput(t, s, protocol.Request{Op: "close-window", Window: 1})
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the last window did not end the session")
	}
}

func TestBackgroundWindowsSendNoFramesButListTheirAgents(t *testing.T) {
	s := windowSession(t)
	mustInput(t, s, protocol.Request{Op: "new-window"}) // pane 2, active
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 1})
	if err := s.reportAgent(2, protocol.AgentBlocked, "", nil); err != nil {
		t.Fatal(err)
	}
	revision := func() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.revisionLocked() }
	// Let the active window's shell finish starting and go idle at a prompt
	// before measuring: it prints a marker, then nothing more.
	mustInput(t, s, protocol.Request{Op: "paste", PaneID: 1, Text: "echo IDLE_$((1+1))\n"})
	s.mu.Lock()
	active := s.panes[1]
	s.mu.Unlock()
	awaitFrame(t, active, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "IDLE_2") })
	before := revision()
	for settled := time.Now(); time.Since(settled) < 300*time.Millisecond; time.Sleep(20 * time.Millisecond) {
		if now := revision(); now != before {
			before, settled = now, time.Now()
		}
	}
	// Only the output has BACK2: a long prompt can wrap the echoed command.
	mustInput(t, s, protocol.Request{Op: "paste", PaneID: 2, Text: "echo BACK$((1+1))\n"})
	s.mu.Lock()
	background := s.panes[2]
	s.mu.Unlock()
	awaitFrame(t, background, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "BACK2") })
	if revision() != before {
		t.Fatal("output in a background window changed the active window's revision")
	}
	// Switching always produces a revision no attachment has seen.
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 2})
	if after := revision(); after <= before {
		t.Fatalf("revision went from %d to %d across a window switch", before, after)
	}
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 1})
	status := s.status()
	if status.Windows != 2 || len(status.Activity) != 1 || status.Activity[0].Pane != 2 || status.Activity[0].State != protocol.AgentBlocked {
		t.Fatalf("status: windows %d activity %+v", status.Windows, status.Activity)
	}
}

func TestRestoreDropsInvalidWindowsAndReadsOldSnapshots(t *testing.T) {
	panes := map[int]paneSnapshot{1: {ID: 1}, 2: {ID: 2}, 3: {ID: 3}}
	windows, active := restoreWindows([]windowSnapshot{
		{Name: "ok", Focused: 9, Layout: &layoutSnapshot{Pane: 1}},
		{Layout: &layoutSnapshot{Pane: 1}}, // pane 1 already shown
		{Name: "bad\x07", Focused: 3, Zoomed: true, Layout: &layoutSnapshot{Direction: "vertical", Ratio: 4000, First: &layoutSnapshot{Pane: 2}, Second: &layoutSnapshot{Pane: 3}}},
	}, 2, panes)
	if len(windows) != 2 || active != 1 {
		t.Fatalf("restored %d windows, active %d", len(windows), active)
	}
	if windows[0].name != "ok" || windows[0].focused != 1 || windows[1].name != "" || windows[1].focused != 3 || !windows[1].zoomed {
		t.Fatalf("windows %+v %+v", windows[0], windows[1])
	}
}
