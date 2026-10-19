package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestDraggingADividerMovesItWithoutFocusOrSelection(t *testing.T) {
	u, requests := sidebarUI(t) // panes 1 (x 0-36) and 2 (x 38-75); divider column 37
	ox := u.sidebarCols()
	mouse := func(x, y int, buttons tcell.ButtonMask) { u.mouse(tcell.NewEventMouse(x, y, buttons, tcell.ModNone)) }

	mouse(ox+37, paneOriginY+2, tcell.Button1)
	noRequest(t, requests)
	mouse(ox+30, paneOriginY+2, tcell.Button1)
	if r := nextRequest(t, requests); r.Op != "drag-divider" || r.PaneID != 1 || r.NeighborID != 2 || r.Direction != "horizontal" || r.X != 30 {
		t.Fatalf("drag: %+v", r)
	}
	mouse(ox+30, paneOriginY+5, tcell.Button1) // same column
	noRequest(t, requests)
	mouse(1, paneOriginY+5, tcell.Button1) // over the sidebar: still the drag
	if r := nextRequest(t, requests); r.Op != "drag-divider" || r.X != 1-ox {
		t.Fatalf("drag over the sidebar: %+v", r)
	}
	mouse(1, paneOriginY+5, tcell.ButtonNone)
	noRequest(t, requests)
	if u.focused != 1 || u.selection.active || u.sidebarFocus {
		t.Fatalf("drag changed focus or selection: focused %d, selection %v", u.focused, u.selection.active)
	}
	// After the release, pane clicks behave normally again.
	mouse(ox+40, paneOriginY+2, tcell.Button1)
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("click after drag: %+v", r)
	}
	mouse(ox+40, paneOriginY+2, tcell.ButtonNone)

	// Stacked panes, with a T-junction where a side-by-side split meets the
	// divider below it: 1 on top, 2 | 3 underneath.
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 76, Height: 3, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("top")}}},
		{ID: 2, Y: 4, Width: 37, Height: 4, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("left")}}},
		{ID: 3, X: 38, Y: 4, Width: 38, Height: 4, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("right")}}},
	}})
	_, height := u.screen.Size()
	mouse(ox+37, paneOriginY+3, tcell.Button1) // the junction
	mouse(ox+37, height-1, tcell.Button1)      // onto the status line
	if r := nextRequest(t, requests); r.Op != "drag-divider" || r.PaneID != 1 || r.NeighborID != 2 || r.Direction != "vertical" || r.Y != height-1-paneOriginY {
		t.Fatalf("stacked drag: %+v", r)
	}
	mouse(ox+37, height-1, tcell.ButtonNone)
	if u.picker {
		t.Fatal("releasing a drag on the status line opened the picker")
	}
	noRequest(t, requests)
	// Zoomed sessions show no dividers to grab.
	u.workspace.Zoomed = true
	mouse(ox+37, paneOriginY+3, tcell.Button1)
	if u.divider != nil {
		t.Fatal("grabbed a divider while zoomed")
	}
}
