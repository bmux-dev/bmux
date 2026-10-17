package client

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestSelectionRepaintInvalidatesCopySource(t *testing.T) {
	s := selection{start: point{0, 10}, end: point{2, 10}, active: true, dragging: true}
	s.remember(&protocol.TerminalFrame{Base: 10, Rows: [][]protocol.Cell{cells("abc")}})
	s.remember(&protocol.TerminalFrame{Base: 10, Rows: [][]protocol.Cell{cells("xyz")}})
	if s.active || s.dragging || s.lines != nil || s.text() != "" {
		t.Fatalf("repaint retained stale selection: %+v", s)
	}
}

func TestSelectionOnlySelectedGraphemesInvalidate(t *testing.T) {
	s := selection{start: point{1, 4}, end: point{2, 4}, active: true}
	s.remember(&protocol.TerminalFrame{Base: 4, Rows: [][]protocol.Cell{cells("abcd")}})
	row := cells("xbcy")
	row[1].FG = "#ff0000"
	s.remember(&protocol.TerminalFrame{Base: 4, Rows: [][]protocol.Cell{row}})
	if !s.active || s.text() != "bc" {
		t.Fatal("unselected repaint or color-only change invalidated selection")
	}
	s.end.x = 3
	if got := s.text(); got != "bcy" {
		t.Fatalf("extended selection used old unselected cells: %q", got)
	}
}

func TestSelectionHistoryCapAnchoring(t *testing.T) {
	s := selection{start: point{0, 100}, end: point{2, 100}, active: true}
	s.remember(&protocol.TerminalFrame{Base: 100, HistoryStart: 90, Rows: [][]protocol.Cell{cells("abc")}})
	// The retained history count can stay capped while absolute row IDs advance.
	s.remember(&protocol.TerminalFrame{Base: 110, HistoryStart: 99, History: 11, Rows: [][]protocol.Cell{cells("xyz")}})
	if !s.active || s.text() != "abc" || s.contains(0, 110) {
		t.Fatal("offscreen scrollback selection moved or disappeared")
	}
	s.remember(&protocol.TerminalFrame{Base: 100, HistoryStart: 100, Rows: [][]protocol.Cell{cells("abc")}})
	if !s.active || !s.contains(0, 100) || s.text() != "abc" {
		t.Fatal("returning to stable scrollback lost selection")
	}
	s.remember(&protocol.TerminalFrame{Base: 111, HistoryStart: 101, Rows: [][]protocol.Cell{cells("new")}})
	if s.active || s.lines != nil || s.text() != "" {
		t.Fatal("genuinely evicted row retained selection or copy source")
	}
}

func TestSelectionScreenIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		old, next protocol.TerminalFrame
	}{
		{"normal reset", protocol.TerminalFrame{Epoch: 1}, protocol.TerminalFrame{Epoch: 2}},
		{"alternate reset", protocol.TerminalFrame{Epoch: 1, Alt: true}, protocol.TerminalFrame{Epoch: 2, Alt: true}},
		{"enter alternate", protocol.TerminalFrame{}, protocol.TerminalFrame{Alt: true}},
		{"leave alternate", protocol.TerminalFrame{Alt: true}, protocol.TerminalFrame{}},
		{"round trip unseen", protocol.TerminalFrame{Epoch: 2}, protocol.TerminalFrame{Epoch: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.old.Rows = [][]protocol.Cell{cells("abc")}
			tc.next.Rows = [][]protocol.Cell{cells("abc")}
			s := selection{start: point{0, 0}, end: point{2, 0}, active: true}
			s.remember(&tc.old)
			s.remember(&tc.next)
			if s.active || s.lines != nil {
				t.Fatal("screen reset retained selection even though glyphs match")
			}
		})
	}
}

func wideWordRow() []protocol.Cell {
	return []protocol.Cell{
		{Text: " ", Width: 1},
		{Text: "世", Width: 2}, {}, {Text: "界", Width: 2}, {},
		{Text: " ", Width: 1}, {Text: " ", Width: 1},
		{Text: "語", Width: 2}, {}, {Text: "!", Width: 1},
	}
}

func TestUnicodeWordBoundsAcrossWideOwners(t *testing.T) {
	row := wideWordRow()
	for _, tc := range []struct{ x, lo, hi int }{
		{0, 0, 0}, {1, 1, 4}, {2, 1, 4}, {3, 1, 4}, {4, 1, 4},
		{5, 5, 6}, {6, 5, 6}, {7, 7, 8}, {8, 7, 8}, {9, 9, 9},
	} {
		lo, hi := wordBounds(row, tc.x)
		if lo != tc.lo || hi != tc.hi {
			t.Errorf("click %d: bounds %d,%d, want %d,%d", tc.x, lo, hi, tc.lo, tc.hi)
		}
	}
}

func TestWideSelectionReverseDragAndHighlight(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		s := selection{start: point{2, 0}, end: point{7, 0}, active: true}
		if reverse {
			s.start, s.end = s.end, s.start
		}
		s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{wideWordRow()}})
		if got := s.text(); got != "世界  語" {
			t.Fatalf("reverse=%v: text %q", reverse, got)
		}
		for x := 0; x < 10; x++ {
			if got, want := s.contains(x, 0), x >= 1 && x <= 8; got != want {
				t.Errorf("reverse=%v: column %d highlighted=%v, want %v", reverse, x, got, want)
			}
		}
	}
}

func TestSelectedWideContinuationRepaint(t *testing.T) {
	s := selection{start: point{2, 0}, end: point{2, 0}, active: true}
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{wideWordRow()}})
	row := wideWordRow()
	row[1].Text = "語"
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{row}})
	if s.active || s.text() != "" {
		t.Fatal("changing owner of selected continuation retained cached glyph")
	}
}

func TestSelectedCombiningGraphemeRepaint(t *testing.T) {
	s := selection{start: point{0, 0}, end: point{0, 0}, active: true}
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{{{Text: "e\u0301", Width: 1}}}})
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{{{Text: "e\u0300", Width: 1}}}})
	if s.active || s.lines != nil {
		t.Fatal("changing combining marks retained a stale selected grapheme")
	}
}

func TestWordBoundsMultipleContinuationColumns(t *testing.T) {
	row := []protocol.Cell{{Text: "界", Width: 3}, {}, {}, {Text: "語", Width: 2}, {}, {Text: " ", Width: 1}}
	for x := 0; x < 5; x++ {
		if lo, hi := wordBounds(row, x); lo != 0 || hi != 4 {
			t.Errorf("column %d: bounds %d,%d, want 0,4", x, lo, hi)
		}
	}
}

func TestConcealedSelectionCopiesVisibleBlanks(t *testing.T) {
	row := []protocol.Cell{
		{Text: "a", Width: 1},
		{Text: "secret", Width: 1, Attrs: uv.AttrConceal},
		{Text: "界", Width: 2, Attrs: uv.AttrConceal}, {},
		{Text: "b", Width: 1},
	}
	s := selection{start: point{0, 0}, end: point{4, 0}, active: true}
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{row}})
	if got := s.text(); got != "a   b" {
		t.Fatalf("copy exposed concealed text or wrong spacing: %q", got)
	}
	row[1].Text = "changed secret"
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{row}})
	if !s.active || s.text() != "a   b" {
		t.Fatal("invisible content change invalidated visible blank selection")
	}
	row[1].Attrs = 0
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{row}})
	if s.active || s.lines != nil {
		t.Fatal("revealing concealed text did not invalidate selected blank")
	}
}

func TestApplyFrameClearsRepaintBeforeDrawing(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(20, 5)
	frame := func(text string) *protocol.Frame {
		return &protocol.Frame{Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 20, Height: 4,
			Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells(text)}}}}}
	}
	u := &ui{screen: screen}
	u.applyFrame(frame("abc"))
	u.selection = selection{start: point{0, 0}, end: point{2, 0}, active: true, dragging: true}
	u.selection.remember(u.frame)
	u.applyFrame(frame("xyz"))
	if u.selection.active || u.selections[1].active || u.selection.text() != "" {
		t.Fatal("incoming frame retained selected or cached stale glyphs")
	}
	u.draw()
	text, style, _ := screen.Get(0, paneOriginY)
	_, bg, _ := style.Decompose()
	if text != "x" || bg == selectionBackground {
		t.Fatalf("new glyph drawn with stale highlight: %q, %v", text, bg)
	}
}
