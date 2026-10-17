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
