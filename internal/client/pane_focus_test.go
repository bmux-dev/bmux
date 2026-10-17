package client

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func focusTestScreen(t *testing.T, width, height int) tcell.SimulationScreen {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(width, height)
	t.Cleanup(s.Fini)
	return s
}

func nestedFocusFrame() *protocol.Frame {
	terminal := func(text string) *protocol.TerminalFrame {
		return &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells(text)}}
	}
	return &protocol.Frame{Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 10, Height: 7, Terminal: terminal("left")},
		{ID: 2, X: 11, Width: 10, Height: 3, Terminal: terminal("upper")},
		{ID: 3, X: 11, Y: 4, Width: 10, Height: 3, Terminal: terminal("lower")},
	}}
}

func TestNestedPaneFocusCuesWithoutCursor(t *testing.T) {
	for _, tc := range []struct {
		id            int
		label         string
		vertical      rune
		horizontal    rune
		firstY, lastY int
	}{
		{1, "pane 1/3 · left", '<', 0, 0, 6},
		{2, "pane 2/3 · upper right", '>', '^', 0, 2},
		{3, "pane 3/3 · lower right", '>', 'v', 4, 6},
	} {
		t.Run(tc.label, func(t *testing.T) {
			s := focusTestScreen(t, 21, 9)
			u := &ui{screen: s}
			f := nestedFocusFrame()
			f.Focused = tc.id
			u.applyFrame(f)
			for _, p := range f.Panes {
				u.drawPane(p, 21, 9)
			}
			u.drawPaneDividers(21, 9)
			if got := u.focusedPaneLabel(); got != tc.label {
				t.Fatalf("label = %q", got)
			}
			verticalMarkers, horizontalMarkers := 0, 0
			for y := 0; y < 7; y++ {
				text, _, _ := s.Get(10, paneOriginY+y)
				if text == string(tc.vertical) {
					verticalMarkers++
					if y < tc.firstY || y > tc.lastY {
						t.Fatalf("arrow identifies wrong pane at y=%d", y)
					}
				}
				if tc.id != 1 && (y < tc.firstY || y > tc.lastY) && text != "│" && text != "┣" {
					t.Fatalf("unfocused shared segment emphasized at y=%d: %q", y, text)
				}
			}
			for x := 11; x < 21; x++ {
				text, _, _ := s.Get(x, paneOriginY+3)
				if tc.horizontal != 0 && text == string(tc.horizontal) {
					horizontalMarkers++
				}
			}
			if verticalMarkers == 0 || tc.horizontal != 0 && horizontalMarkers == 0 {
				t.Fatalf("missing non-color cues: vertical=%d horizontal=%d", verticalMarkers, horizontalMarkers)
			}
		})
	}
}

func TestPaneContentStyleOriginAndFooterBounds(t *testing.T) {
	s := focusTestScreen(t, 6, 4)
	u := &ui{screen: s}
	c := protocol.Cell{Text: "X", Width: 1, FG: "#ff00ff", BG: "#123456", Attrs: uv.AttrBold | uv.AttrItalic}
	f := &protocol.Frame{Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 6, Height: 4, Terminal: &protocol.TerminalFrame{
		Rows:          [][]protocol.Cell{{c}, cells("rowtwo"), cells("rowtri"), cells("BAD")},
		CursorVisible: true, CursorX: 2, CursorY: 0,
	}}}}
	u.applyFrame(f)
	for x := 0; x < 6; x++ {
		s.Put(x, 3, "F", barStyle)
	}
	u.drawPane(f.Panes[0], 6, 4)
	u.drawPaneDividers(6, 4)
	text, style, _ := s.Get(0, paneOriginY)
	if text != "X" || style != cellStyle(c) {
		t.Fatalf("application cell changed: %q %v", text, style)
	}
	x, y, visible := s.GetCursor()
	if !visible || x != 2 || y != paneOriginY {
		t.Fatalf("cursor origin: %d,%d visible=%v", x, y, visible)
	}
	for x := 0; x < 6; x++ {
		text, _, _ := s.Get(x, 3)
		if text != "F" {
			t.Fatalf("footer overwritten at x=%d", x)
		}
	}
	if got := (&ui{screen: s}).focusedPlacement().Height; got != 2 {
		t.Fatalf("fallback height = %d", got)
	}
}

func TestPaneDividersNeverOverwriteApplicationRectangles(t *testing.T) {
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := nestedFocusFrame()
	u.applyFrame(f)
	// Fill even empty application cells; chrome must not use them for badges.
	for _, p := range f.Panes {
		for y := p.Y; y < p.Y+p.Height; y++ {
			for x := p.X; x < p.X+p.Width; x++ {
				s.Put(x, paneOriginY+y, "A", tcell.StyleDefault)
			}
		}
	}
	u.drawPaneDividers(21, 9)
	for _, p := range f.Panes {
		for y := p.Y; y < p.Y+p.Height; y++ {
			for x := p.X; x < p.X+p.Width; x++ {
				text, style, _ := s.Get(x, paneOriginY+y)
				if text != "A" || style != tcell.StyleDefault {
					t.Fatalf("application cell overwritten at %d,%d", x, y)
				}
			}
		}
	}
}

func TestTinyAndCollapsedPaneFocus(t *testing.T) {
	s := focusTestScreen(t, 3, 3)
	u := &ui{screen: s}
	f := &protocol.Frame{Focused: 2, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 1, Height: 1}, {ID: 2, X: 2, Width: 1, Height: 1},
	}}
	u.applyFrame(f)
	u.drawPaneDividers(3, 3)
	text, _, _ := s.Get(1, paneOriginY)
	if text != ">" {
		t.Fatalf("tiny pane lacks ASCII focus cue: %q", text)
	}
	f.Panes[1].Width = 0
	u.applyFrame(f)
	if !strings.HasPrefix(u.focusedPaneLabel(), "pane 2/2") {
		t.Fatal("collapsed pane lost footer identification")
	}
	u.drawPaneDividers(3, 3)
}

func TestFocusAfterRemovalAndZoom(t *testing.T) {
	u := &ui{}
	u.applyFrame(nestedFocusFrame())
	u.activatePane(2)
	u.selection = selection{active: true}
	u.selection.remember(u.frame)
	zoom := nestedFocusFrame()
	zoom.Focused, zoom.Zoomed = 2, true
	zoom.Panes = zoom.Panes[1:2]
	u.applyFrame(zoom)
	if u.focused != 2 || !u.selection.active {
		t.Fatal("zoom discarded unchanged focused selection")
	}
	f := nestedFocusFrame()
	f.Panes = append(f.Panes[:1], f.Panes[2:]...)
	f.Focused = 3
	u.applyFrame(f)
	if u.focused != 3 || u.frame != f.Panes[1].Terminal || u.selection.active || u.focusedPaneLabel() != "pane 2/2 · lower right" {
		t.Fatal("removed pane left stale focus or selection")
	}
	if _, ok := u.paneSizes[2]; ok {
		t.Fatal("removed pane retained size cache")
	}
	// Be defensive if a transient frame carries a removed focus identifier.
	f.Focused = 99
	u.focused = 99
	u.applyFrame(f)
	if u.focused != 1 || u.frame != f.Panes[0].Terminal {
		t.Fatal("invalid focus retained a stale terminal")
	}
}

func TestExitedAndZoomedPaneFocusStaysInChrome(t *testing.T) {
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := nestedFocusFrame()
	f.Focused = 2
	f.Panes[1].Terminal.Exited = true
	f.Panes[1].Terminal.CursorVisible = true
	u.applyFrame(f)
	s.HideCursor()
	for _, p := range f.Panes {
		u.drawPane(p, 21, 9)
	}
	u.drawPaneDividers(21, 9)
	if _, _, visible := s.GetCursor(); visible {
		t.Fatal("exited terminal displayed a cursor")
	}
	text, _, _ := s.Get(10, paneOriginY+1)
	if text != ">" {
		t.Fatalf("exited pane lost focus cue: %q", text)
	}
	f.Zoomed = true
	f.Panes = f.Panes[1:2]
	f.Panes[0].X, f.Panes[0].Y = 0, 0
	f.Panes[0].Width, f.Panes[0].Height = 21, 7
	u.applyFrame(f)
	s.Clear()
	u.drawPane(f.Panes[0], 21, 9)
	u.drawPaneDividers(21, 9)
	if u.focusedPaneLabel() != "pane 1/1" || u.focused != 2 {
		t.Fatal("zoomed pane lost footer focus identification")
	}
	text, _, _ = s.Get(0, paneOriginY)
	if text != "u" {
		t.Fatal("zoom chrome overwrote application text")
	}
	// At height one only chrome is available; rendering must stay out of it.
	s.SetSize(1, 1)
	s.Put(0, 0, "F", barStyle)
	u.drawPane(f.Panes[0], 1, 1)
	u.drawPaneDividers(1, 1)
	text, _, _ = s.Get(0, 0)
	if text != "F" {
		t.Fatal("tiny viewport overwrote footer")
	}
}
