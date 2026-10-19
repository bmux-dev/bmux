package server

import "testing"

func TestRemovePanePromotesSiblingAndPreservesNestedRatios(t *testing.T) {
	root := &layout{direction: "horizontal", ratio: 4000, first: &layout{id: 1}, second: &layout{direction: "vertical", ratio: 3000, first: &layout{id: 2}, second: &layout{id: 3}}}
	root, neighbor := root.remove(1)
	if neighbor != 2 || root.direction != "vertical" || root.ratio != 3000 {
		t.Fatalf("root sibling not promoted: %+v, %d", root, neighbor)
	}
	var positions []placement
	root.place(rect{0, 0, 81, 21}, &positions)
	if positions[0].w != 81 || positions[0].h != 6 || positions[1].h != 14 {
		t.Fatalf("nested proportions lost: %+v", positions)
	}
	root, neighbor = root.remove(2)
	if root.id != 3 || neighbor != 3 {
		t.Fatal("nested leaf removal failed")
	}
	root, _ = root.remove(3)
	if root != nil {
		t.Fatal("last pane retained a layout")
	}
}

func TestNestedLayoutAndResize(t *testing.T) {
	root := &layout{direction: "horizontal", ratio: 5000, first: &layout{id: 1}, second: &layout{direction: "vertical", ratio: 5000, first: &layout{id: 2}, second: &layout{id: 3}}}
	w, h := root.minimum()
	if w != 5 || h != 3 {
		t.Fatalf("minimum=%d,%d", w, h)
	}
	var positions []placement
	root.place(rect{0, 0, 81, 25}, &positions)
	want := []placement{{1, rect{0, 0, 40, 25}}, {2, rect{41, 0, 40, 12}}, {3, rect{41, 13, 40, 12}}}
	for i, p := range positions {
		if p != want[i] {
			t.Fatalf("pane %d: %+v want %+v", i, p, want[i])
		}
	}
	if !root.resize(3, "up", 5, rect{0, 0, 81, 25}) {
		t.Fatal("vertical resize not applied")
	}
	positions = nil
	root.place(rect{0, 0, 81, 25}, &positions)
	if positions[1].h != 7 || positions[2].y != 8 || positions[2].h != 17 {
		t.Fatalf("resized layout: %+v", positions)
	}
	if !root.resize(3, "right", 5, rect{0, 0, 81, 25}) {
		t.Fatal("ancestor divider not found")
	}
	positions = nil
	root.place(rect{0, 0, 81, 25}, &positions)
	if positions[0].w != 45 || positions[2].w != 35 {
		t.Fatalf("horizontal resize: %+v", positions)
	}
	for i := 0; i < 100; i++ {
		root.resize(3, "up", 5, rect{0, 0, 81, 25})
	}
	positions = nil
	root.place(rect{0, 0, 81, 25}, &positions)
	if positions[1].h < 1 || positions[2].h < 1 {
		t.Fatalf("pane resized below minimum: %+v", positions)
	}
}

func TestSetDividerMovesTheSplitBetweenTwoPanes(t *testing.T) {
	// 1 | (2 / (3 | 4)) in 81x25: the root divider is column 40.
	root := &layout{direction: "horizontal", ratio: 5000, first: &layout{id: 1}, second: &layout{direction: "vertical", ratio: 5000,
		first: &layout{id: 2}, second: &layout{direction: "horizontal", ratio: 5000, first: &layout{id: 3}, second: &layout{id: 4}}}}
	area := rect{0, 0, 81, 25}
	place := func() map[int]rect {
		var positions []placement
		root.place(area, &positions)
		result := make(map[int]rect)
		for _, p := range positions {
			result[p.id] = p.rect
		}
		return result
	}
	// Panes 1 and 3 meet only across the root divider.
	if !root.setDivider(1, 3, "horizontal", 30, area) {
		t.Fatal("root divider not found from panes 1 and 3")
	}
	if p := place(); p[1].w != 30 || p[2].x != 31 || p[3].x != 31 {
		t.Fatalf("root divider not at column 30: %+v", p)
	}
	// The nested stacked divider is row 12; moving it keeps the root's.
	if !root.setDivider(2, 4, "vertical", 5, area) {
		t.Fatal("stacked divider not found")
	}
	if p := place(); p[2].h != 5 || p[3].y != 6 || p[1].w != 30 {
		t.Fatalf("stacked divider: %+v", p)
	}
	// The innermost divider is placed relative to its own split's origin.
	if !root.setDivider(3, 4, "horizontal", 70, area) {
		t.Fatal("inner divider not found")
	}
	if p := place(); p[3].w != 70-31 || p[4].x != 71 {
		t.Fatalf("inner divider: %+v", p)
	}
	// Dragging past the edge stops at the minimum pane size.
	root.setDivider(1, 2, "horizontal", 500, area)
	if p := place(); p[3].w < 2 || p[4].w < 2 || p[1].w+1+p[3].w+1+p[4].w != 81 {
		t.Fatalf("divider pushed a pane below its minimum: %+v", p)
	}
	root.setDivider(1, 2, "horizontal", -10, area)
	if p := place(); p[1].w != 2 {
		t.Fatalf("divider dragged past the left edge: %+v", p)
	}
	for _, bad := range []struct {
		first, second int
		direction     string
	}{{3, 1, "horizontal"}, {1, 3, "vertical"}, {1, 9, "horizontal"}, {2, 2, "vertical"}} {
		before := place()
		if root.setDivider(bad.first, bad.second, bad.direction, 20, area) {
			t.Fatalf("%+v moved a divider", bad)
		}
		if after := place(); after[1] != before[1] || after[3] != before[3] {
			t.Fatalf("%+v changed the layout", bad)
		}
	}
}

func TestLayoutOnTinyTerminal(t *testing.T) {
	root := &layout{direction: "horizontal", ratio: 5000, first: &layout{id: 1}, second: &layout{direction: "vertical", ratio: 5000, first: &layout{id: 2}, second: &layout{id: 3}}}
	for w := 2; w < 30; w++ {
		for h := 1; h < 15; h++ {
			var positions []placement
			root.place(rect{0, 0, w, h}, &positions)
			occupied := make(map[[2]int]int)
			for _, p := range positions {
				if p.w < 0 || p.h < 0 {
					t.Fatalf("negative rectangle: %+v", p)
				}
				for y := p.y; y < p.y+p.h; y++ {
					for x := p.x; x < p.x+p.w; x++ {
						if x >= w || y >= h {
							t.Fatalf("out of bounds at %dx%d: %+v", w, h, p)
						}
						key := [2]int{x, y}
						if occupied[key] != 0 {
							t.Fatal("overlapping panes")
						}
						occupied[key] = p.id
					}
				}
			}
		}
	}
}
