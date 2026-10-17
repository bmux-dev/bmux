package client

import (
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func junctionCell(text string) *protocol.TerminalFrame {
	return &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells(text)}}
}

// The reported layout: two panes stacked on the left, one full-height pane on
// the right. The divider column belongs to the two left panes' right edges,
// which stop on either side of the horizontal divider's row, so the junction
// cell belongs to no pane edge yet must not interrupt the border.
func junctionTestFrame() *protocol.Frame {
	return &protocol.Frame{Focused: 3, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 10, Height: 3, Terminal: junctionCell("upper-left")},
		{ID: 2, Y: 4, Width: 10, Height: 3, Terminal: junctionCell("lower-left")},
		{ID: 3, X: 11, Width: 10, Height: 7, Terminal: junctionCell("full-right")},
	}}
}

func TestDividerContinuesThroughUnownedJunctionCell(t *testing.T) {
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := junctionTestFrame()
	u.applyFrame(f)
	for _, p := range f.Panes {
		u.drawPane(p, 21, 9)
	}
	u.drawPaneDividers(21, 9)
	for y := 0; y < 7; y++ {
		if text, _, _ := s.Get(10, paneOriginY+y); text == " " || text == "" {
			t.Fatalf("divider gap in junction column at y=%d", y)
		}
	}
	// The horizontal row ends against the full-height pane, so the junction
	// carries the row's arm: a T connector, not a bare bar.
	if text, _, _ := s.Get(10, paneOriginY+3); text != "┫" {
		t.Fatalf("junction glyph = %q, want ┫", text)
	}
	for x := 0; x < 10; x++ {
		if text, _, _ := s.Get(x, paneOriginY+3); text == " " || text == "" {
			t.Fatalf("divider gap in horizontal row at x=%d", x)
		}
	}
}

// A highlighted junction keeps every arm, even one leading into a muted line,
// so the joint never shows a gap.
func TestFocusedJunctionKeepsAllArms(t *testing.T) {
	for _, focused := range []int{1, 2, 3} {
		s := focusTestScreen(t, 21, 9)
		u := &ui{screen: s}
		f := &protocol.Frame{Focused: focused, Panes: []protocol.PaneFrame{
			{ID: 1, Width: 10, Height: 7, Terminal: junctionCell("left")},
			{ID: 2, X: 11, Width: 10, Height: 3, Terminal: junctionCell("upper-right")},
			{ID: 3, X: 11, Y: 4, Width: 10, Height: 3, Terminal: junctionCell("lower-right")},
		}}
		u.applyFrame(f)
		for _, p := range f.Panes {
			u.drawPane(p, 21, 9)
		}
		u.drawPaneDividers(21, 9)
		if text, _, _ := s.Get(10, paneOriginY+3); text != "┣" {
			t.Fatalf("focused %d: junction glyph = %q, want ┣", focused, text)
		}
		// The highlight stays on the focused pane's edge: the other right-hand
		// pane's stretch of the column keeps the muted line.
		other := map[int]int{2: 5, 3: 1}
		if y, ok := other[focused]; ok {
			if text, _, _ := s.Get(10, paneOriginY+y); text != "│" {
				t.Fatalf("focused %d: column at y=%d = %q, want muted │", focused, y, text)
			}
		}
	}
}

func TestUnfocusedJunctionKeepsAllArms(t *testing.T) {
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := junctionTestFrame()
	f.Focused = 0
	u.applyFrame(f)
	for _, p := range f.Panes {
		u.drawPane(p, 21, 9)
	}
	u.drawPaneDividers(21, 9)
	if text, _, _ := s.Get(10, paneOriginY+3); text != "┤" {
		t.Fatalf("junction glyph = %q, want ┤", text)
	}
}

func TestFocusedRowJunctionJoinsColumn(t *testing.T) {
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := junctionTestFrame()
	f.Focused = 1
	u.applyFrame(f)
	for _, p := range f.Panes {
		u.drawPane(p, 21, 9)
	}
	u.drawPaneDividers(21, 9)
	if text, _, _ := s.Get(10, paneOriginY+3); text != "┫" {
		t.Fatalf("junction glyph = %q, want ┫", text)
	}
}

func TestDividerCrossJunctionGlyph(t *testing.T) {
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := &protocol.Frame{Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 10, Height: 3, Terminal: junctionCell("top-left")},
		{ID: 2, X: 11, Width: 10, Height: 3, Terminal: junctionCell("top-right")},
		{ID: 3, Y: 4, Width: 10, Height: 3, Terminal: junctionCell("bottom-left")},
		{ID: 4, X: 11, Y: 4, Width: 10, Height: 3, Terminal: junctionCell("bottom-right")},
	}}
	u.applyFrame(f)
	for _, p := range f.Panes {
		u.drawPane(p, 21, 9)
	}
	u.drawPaneDividers(21, 9)
	if text, _, _ := s.Get(10, paneOriginY+3); text != "╋" {
		t.Fatalf("cross junction glyph = %q, want ╋", text)
	}
	for y := 0; y < 3; y++ {
		if text, _, _ := s.Get(10, paneOriginY+y); text == " " || text == "" {
			t.Fatalf("vertical divider gap above the cross at y=%d", y)
		}
		if text, _, _ := s.Get(10, paneOriginY+4+y); text == " " || text == "" {
			t.Fatalf("vertical divider gap below the cross at y=%d", y)
		}
	}
	for x := 0; x < 21; x++ {
		if x == 10 {
			continue
		}
		if text, _, _ := s.Get(x, paneOriginY+3); text == " " || text == "" {
			t.Fatalf("horizontal divider gap at x=%d", x)
		}
	}
}
