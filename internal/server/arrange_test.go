package server

import (
	"reflect"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func leafIDs(w *window) []int {
	var ids []int
	w.root.leaves(&ids)
	return ids
}

func TestSwapRotateAndLastPaneMovePanesNotProcesses(t *testing.T) {
	s := windowSession(t)
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // 1 | 2
	mustInput(t, s, protocol.Request{Op: "split", Direction: "vertical"})   // 1 | 2/3, focus 3
	s.mu.Lock()
	shells := map[int]*pane{1: s.panes[1], 2: s.panes[2], 3: s.panes[3]}
	before := snapshotLayout(s.windows[0].root)
	s.mu.Unlock()

	check := func(order []int, focused int) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		w := s.windows[0]
		if got := leafIDs(w); !reflect.DeepEqual(got, order) || w.focused != focused {
			t.Fatalf("order %v focus %d, want %v focus %d", got, w.focused, order, focused)
		}
		for id, p := range shells {
			if s.panes[id] != p {
				t.Fatalf("pane %d was replaced", id)
			}
		}
		// Only the panes move; every split keeps its direction and ratio.
		after := snapshotLayout(w.root)
		var strip func(l *layoutSnapshot)
		strip = func(l *layoutSnapshot) {
			if l.Pane != 0 {
				l.Pane = 1
				return
			}
			strip(l.First)
			strip(l.Second)
		}
		a, b := *before, *after
		strip(&a)
		strip(&b)
		if !reflect.DeepEqual(a, b) {
			t.Fatal("moving panes changed the splits")
		}
	}
	mustInput(t, s, protocol.Request{Op: "swap-pane", Direction: "previous"})
	check([]int{1, 3, 2}, 3)
	mustInput(t, s, protocol.Request{Op: "swap-pane", Direction: "previous"})
	check([]int{3, 1, 2}, 3)
	mustInput(t, s, protocol.Request{Op: "swap-pane", Direction: "previous"}) // wraps to the end
	check([]int{2, 1, 3}, 3)
	mustInput(t, s, protocol.Request{Op: "rotate-window"})
	check([]int{3, 2, 1}, 3)

	mustInput(t, s, protocol.Request{Op: "select-pane", PaneID: 1})
	mustInput(t, s, protocol.Request{Op: "last-pane"})
	check([]int{3, 2, 1}, 3)
	mustInput(t, s, protocol.Request{Op: "last-pane"})
	check([]int{3, 2, 1}, 1)

	mustInput(t, s, protocol.Request{Op: "zoom"})
	for _, r := range []protocol.Request{{Op: "swap-pane", Direction: "next"}, {Op: "rotate-window"}} {
		if err := s.input(r); err == nil {
			t.Fatalf("%s while zoomed", r.Op)
		}
	}
	if err := s.input(protocol.Request{Op: "swap-pane", Direction: "up"}); err == nil {
		t.Fatal("unknown swap direction accepted")
	}
	// The last pane closing leaves last-pane nothing to do.
	mustInput(t, s, protocol.Request{Op: "zoom"})
	mustInput(t, s, protocol.Request{Op: "close-pane", PaneID: 3})
	awaitPaneGone(t, s, 3)
	mustInput(t, s, protocol.Request{Op: "last-pane"})
	check2 := func() int { s.mu.Lock(); defer s.mu.Unlock(); return s.windows[0].focused }
	if got := check2(); got != 1 {
		t.Fatalf("last-pane to a closed pane moved focus to %d", got)
	}
}

func TestPresetLayoutsPlaceEveryPaneWithoutOverlap(t *testing.T) {
	area := rect{0, 0, 160, 40}
	for _, name := range layoutPresets {
		for n := 1; n <= maxPanes; n++ {
			ids := make([]int, n)
			for i := range ids {
				ids[i] = i + 10
			}
			root, err := presetLayout(name, ids)
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			root.leaves(&got)
			if !reflect.DeepEqual(got, ids) {
				t.Fatalf("%s with %d panes reordered them: %v", name, n, got)
			}
			var positions []placement
			root.place(area, &positions)
			mw, mh := root.minimum()
			fits := mw <= area.w && mh <= area.h
			if name == "tiled" && !fits {
				t.Fatalf("tiled with %d panes doesn't fit %dx%d", n, area.w, area.h)
			}
			cells := 0
			for i, a := range positions {
				if fits && (a.w < 2 || a.h < 1) {
					t.Fatalf("%s with %d panes: pane %d is %dx%d", name, n, a.id, a.w, a.h)
				}
				cells += a.w * a.h
				for _, b := range positions[i+1:] {
					if a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h {
						t.Fatalf("%s with %d panes: %d and %d overlap", name, n, a.id, b.id)
					}
				}
			}
			if cells > area.w*area.h {
				t.Fatalf("%s with %d panes overflows", name, n)
			}
		}
	}
	if _, err := presetLayout("diagonal", []int{1, 2}); err == nil {
		t.Fatal("unknown layout accepted")
	}
}

func TestEvenAndEqualizedLayoutsShareSpace(t *testing.T) {
	widths := func(root *layout) []int {
		var positions []placement
		root.place(rect{0, 0, 120, 30}, &positions)
		var result []int
		for _, p := range positions {
			result = append(result, p.w)
		}
		return result
	}
	even, _ := presetLayout("even-horizontal", []int{1, 2, 3, 4})
	// Lopsided splits made one at a time, then spread evenly.
	lopsided := &layout{direction: "horizontal", ratio: 8000, first: &layout{id: 1},
		second: &layout{direction: "horizontal", ratio: 1000, first: &layout{id: 2}, second: &layout{direction: "horizontal", ratio: 5000, first: &layout{id: 3}, second: &layout{id: 4}}}}
	lopsided.equalize()
	for _, root := range []*layout{even, lopsided} {
		got := widths(root)
		for _, w := range got {
			if w < 28 || w > 30 {
				t.Fatalf("widths %v aren't even", got)
			}
		}
	}
	// Equalizing keeps a stack inside a column one share of the row.
	mixed := &layout{direction: "horizontal", ratio: 9000, first: &layout{direction: "vertical", ratio: 2000, first: &layout{id: 1}, second: &layout{id: 2}}, second: &layout{id: 3}}
	mixed.equalize()
	if mixed.ratio != 5000 || mixed.first.ratio != 5000 {
		t.Fatalf("ratios %d / %d", mixed.ratio, mixed.first.ratio)
	}
}

func TestSelectLayoutCyclesPresetsAndUnzooms(t *testing.T) {
	s := windowSession(t)
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"})
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"})
	mustInput(t, s, protocol.Request{Op: "zoom"})
	for _, want := range append(layoutPresets, layoutPresets[0]) {
		mustInput(t, s, protocol.Request{Op: "select-layout", Kind: "next"})
		s.mu.Lock()
		w := s.windows[0]
		expected, _ := presetLayout(want, []int{1, 2, 3})
		got, zoomed := snapshotLayout(w.root), w.zoomed
		s.mu.Unlock()
		if !reflect.DeepEqual(got, snapshotLayout(expected)) || zoomed {
			t.Fatalf("next layout should be %s: %+v zoomed %v", want, got, zoomed)
		}
	}
	if err := s.input(protocol.Request{Op: "select-layout", Kind: "spiral"}); err == nil {
		t.Fatal("unknown layout accepted")
	}
}

func TestBreakAndJoinPanesKeepTheirProcess(t *testing.T) {
	s := windowSession(t)
	if err := s.input(protocol.Request{Op: "break-pane"}); err == nil {
		t.Fatal("broke a window's only pane")
	}
	if err := s.input(protocol.Request{Op: "join-pane"}); err == nil {
		t.Fatal("joined without a last window")
	}
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // 1 | 2, focus 2
	s.mu.Lock()
	shell := s.panes[2]
	s.mu.Unlock()

	mustInput(t, s, protocol.Request{Op: "break-pane"})
	f := snapshot(s)
	if got := windowPaneIDs(f); !reflect.DeepEqual(got, [][]int{{1}, {2}}) || activeWindow(f) != 2 || f.Focused != 2 {
		t.Fatalf("after break: %v active %d focus %d", got, activeWindow(f), f.Focused)
	}
	mustInput(t, s, protocol.Request{Op: "new-window"}) // 3: pane 3; last window is 2
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 2})
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 1})
	// Join pane 1 into window 2 (the last window); window 1 empties and closes.
	mustInput(t, s, protocol.Request{Op: "join-pane"})
	f = snapshot(s)
	if got := windowPaneIDs(f); !reflect.DeepEqual(got, [][]int{{2, 1}, {3}}) || activeWindow(f) != 1 || f.Focused != 1 {
		t.Fatalf("after join: %v active %d focus %d", got, activeWindow(f), f.Focused)
	}
	s.mu.Lock()
	same, last := s.panes[2] == shell, s.lastWindow
	s.mu.Unlock()
	if !same {
		t.Fatal("moving a pane replaced its process")
	}
	if last != nil {
		t.Fatal("the emptied window is still the last window")
	}
	// Join back the other way: pane 3's window empties, pane 3 lands beside 1.
	mustInput(t, s, protocol.Request{Op: "select-window", Window: 2})
	mustInput(t, s, protocol.Request{Op: "join-pane"})
	if got := windowPaneIDs(snapshot(s)); !reflect.DeepEqual(got, [][]int{{2, 1, 3}}) {
		t.Fatalf("after second join: %v", got)
	}
}
