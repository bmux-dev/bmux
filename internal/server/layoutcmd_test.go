package server

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func layoutCommand(t *testing.T, s *session, r protocol.Request) protocol.PaneActivity {
	t.Helper()
	r.Op = "layout-command"
	a, err := s.layoutCommand(r)
	if err != nil {
		t.Fatalf("%s: %v", r.Kind, err)
	}
	return a
}

func awaitPaneText(t *testing.T, s *session, id int, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, text, err := s.capturePane(paneTarget{pane: id})
		if err == nil && strings.Contains(text, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %d never showed %q:\n%s", id, want, text)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLayoutCommandsDriveTheLayoutFromATarget(t *testing.T) {
	s := windowSession(t)
	dir := realDir(t, t.TempDir())

	// split: in DIR, with a command, focused unless -d.
	a := layoutCommand(t, s, protocol.Request{Kind: "split", PaneID: 1, Direction: "horizontal", Dir: dir, Args: []string{"echo SPLIT_$((2+2))"}})
	if a.Pane != 2 || a.Window != 1 || a.Index != 2 {
		t.Fatalf("split: %+v", a)
	}
	awaitCwd(t, s.panes[2], dir)
	awaitPaneText(t, s, 2, "SPLIT_4")
	s.mu.Lock()
	focused := s.focusedLocked()
	s.mu.Unlock()
	if focused != 2 {
		t.Fatalf("split moved focus to %d, want the new pane", focused)
	}
	a = layoutCommand(t, s, protocol.Request{Kind: "split", PaneID: 1, Direction: "vertical", Detach: true})
	s.mu.Lock()
	focused = s.focusedLocked()
	var ids []int
	s.windows[0].root.leaves(&ids)
	s.mu.Unlock()
	if a.Pane != 3 || focused != 2 || !reflect.DeepEqual(ids, []int{1, 3, 2}) {
		t.Fatalf("split -d: new %+v, focus %d, layout %v", a, focused, ids)
	}

	// new-window: named, argv quoted, and -d keeps the active window.
	a = layoutCommand(t, s, protocol.Request{Kind: "new-window", PaneID: 1, NewName: "logs", Detach: true, Args: []string{"echo", "NEW WINDOW"}})
	s.mu.Lock()
	active, name := s.active, s.windows[len(s.windows)-1].name
	s.mu.Unlock()
	if a.Pane != 4 || a.Window != 2 || active != 0 || name != "logs" {
		t.Fatalf("new-window -d: %+v, active %d, name %q", a, active, name)
	}
	awaitPaneText(t, s, 4, "NEW WINDOW")

	// select: a pane brings its window forward; a window target selects it.
	layoutCommand(t, s, protocol.Request{Kind: "select", PaneID: 4})
	s.mu.Lock()
	active, focused = s.active, s.focusedLocked()
	s.mu.Unlock()
	if active != 1 || focused != 4 {
		t.Fatalf("select pane 4: window %d, pane %d", active+1, focused)
	}
	layoutCommand(t, s, protocol.Request{Kind: "select", Window: 1})
	s.mu.Lock()
	active = s.active
	s.mu.Unlock()
	if active != 0 {
		t.Fatalf("select window 1: window %d", active+1)
	}

	// zoom and rename-window act on the target's window.
	layoutCommand(t, s, protocol.Request{Kind: "zoom", PaneID: 3})
	layoutCommand(t, s, protocol.Request{Kind: "rename-window", PaneID: 4, NewName: "tail"})
	s.mu.Lock()
	zoomed, focused, name := s.windows[0].zoomed, s.windows[0].focused, s.windows[1].name
	s.mu.Unlock()
	if !zoomed || focused != 3 || name != "tail" {
		t.Fatalf("zoom pane 3: zoomed %v focus %d; rename: %q", zoomed, focused, name)
	}
	layoutCommand(t, s, protocol.Request{Kind: "zoom", PaneID: 3})

	// resize moves the divider by the cells asked for.
	width := func(id int) int {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, p := range s.windows[0].placements(s.width, s.height) {
			if p.id == id {
				return p.rect.w
			}
		}
		return 0
	}
	before := width(2)
	layoutCommand(t, s, protocol.Request{Kind: "resize", PaneID: 2, Direction: "left", Delta: 7})
	if got := width(2); got != before+7 {
		t.Fatalf("resize left 7: pane 2 width %d, was %d", got, before)
	}

	// kill-pane and kill-window end their shells; the layout follows.
	layoutCommand(t, s, protocol.Request{Kind: "kill-pane", PaneID: 3})
	layoutCommand(t, s, protocol.Request{Kind: "kill-window", Window: 2})
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		ids = ids[:0]
		s.windows[0].root.leaves(&ids)
		windows := len(s.windows)
		s.mu.Unlock()
		if windows == 1 && reflect.DeepEqual(ids, []int{1, 2}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after kill-pane 3 and kill-window 2: %d windows, window 1 %v", windows, ids)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Mistakes are reported, never run somewhere else.
	for _, r := range []protocol.Request{
		{Kind: "split", PaneID: 9},
		{Kind: "kill-pane", Window: 1},
		{Kind: "resize", Window: 1, Direction: "left"},
		{Kind: "split", PaneID: 1, Dir: "relative"},
		{Kind: "split", PaneID: 1, Dir: filepath.Join(dir, "missing")},
		{Kind: "fly", PaneID: 1},
	} {
		r.Op = "layout-command"
		if _, err := s.layoutCommand(r); err == nil {
			t.Errorf("%+v was accepted", r)
		}
	}
}
