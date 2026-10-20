package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestDragPastThePaneScrollsAndExtendsTheSelection(t *testing.T) {
	u, requests := viewUI(t)
	ox := u.paneOriginX(100)
	mouse := func(x, y int, buttons tcell.ButtonMask) {
		u.mouse(tcell.NewEventMouse(ox+x, y, buttons, tcell.ModNone))
	}
	mouse(2, paneOriginY+2, tcell.Button1) // row 102
	mouse(2, paneOriginY+1, tcell.Button1)
	// Just above the pane: one row at once, and the end reaches the top row.
	mouse(2, 0, tcell.Button1)
	if r := nextRequest(t, requests); r.Op != "scroll" || r.Delta != 1 {
		t.Fatalf("scroll %+v", r)
	}
	if !u.drag.active || u.selection.end != (point{2, 100}) {
		t.Fatalf("drag %+v end %+v", u.drag, u.selection.end)
	}
	// The frame it scrolled to moves the end onto its new top row.
	rows := [][]protocol.Cell{cells("zero"), cells("one two three"), cells("four five"), cells("six"), cells("seven")}
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 13, Height: 5, Terminal: &protocol.TerminalFrame{Rows: rows, Base: 99, Epoch: 3}},
	}})
	if u.selection.end != (point{2, 99}) || u.selection.text() != "ro\none two three\nfour five\nsix" {
		t.Fatalf("after frame: end %+v text %q", u.selection.end, u.selection.text())
	}
	// Held still, it keeps scrolling.
	u.dragScrollTick()
	if r := nextRequest(t, requests); r.Op != "scroll" || r.Delta != 1 {
		t.Fatalf("tick %+v", r)
	}
	// Further out scrolls faster: three rows below the pane, two rows a tick down.
	mouse(20, paneOriginY+5+3, tcell.Button1)
	if u.drag.rows != -2 || u.selection.end != (point{12, 103}) {
		t.Fatalf("below: drag %+v end %+v", u.drag, u.selection.end)
	}
	// Back inside the pane stops scrolling; releasing copies.
	mouse(1, paneOriginY+1, tcell.Button1)
	if u.drag.active || u.drag.ticker != nil || u.dragScrollC() != nil || u.selection.end != (point{1, 100}) {
		t.Fatalf("back inside: drag %+v end %+v", u.drag, u.selection.end)
	}
	mouse(20, paneOriginY+8, tcell.Button1)
	mouse(20, paneOriginY+8, tcell.ButtonNone)
	if u.drag.active || u.drag.ticker != nil || u.selection.dragging {
		t.Fatalf("release left drag %+v dragging %v", u.drag, u.selection.dragging)
	}
}

func TestCopyFetchesRowsAutoScrollSkipped(t *testing.T) {
	u, _ := viewUI(t)
	socket, rpcs := rpcServer(t, func(protocol.Request) protocol.Response { return protocol.Response{Text: "from history"} })
	u.socket = socket
	// Rows 100-104 arrived in frames; rows 50-99 scrolled past unseen.
	u.selection = selection{start: point{3, 101}, end: point{0, 50}, active: true}
	u.selection.remember(u.frame)
	u.copy()
	r := <-rpcs
	if r.Op != "history-text" || r.PaneID != 1 || r.Range == nil ||
		*r.Range != (protocol.TextRange{Epoch: 3, FromX: 0, FromY: 50, ToX: 3, ToY: 101, Mode: "char"}) {
		t.Fatalf("copy request %+v range %+v", r, r.Range)
	}
}
