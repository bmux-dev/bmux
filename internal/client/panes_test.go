package client

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func splitFrame() *protocol.Frame {
	return &protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, X: 0, Y: 0, Width: 10, Height: 6, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("left pane")}, CursorVisible: true, CursorX: 2, CursorY: 0}},
		{ID: 2, X: 11, Y: 0, Width: 10, Height: 6, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("right pane")}, CursorVisible: true, CursorX: 3, CursorY: 0}},
	}}
}

func TestClosedPaneClearsSelectionAndTransientInputState(t *testing.T) {
	u := &ui{}
	u.applyFrame(splitFrame())
	u.selection = selection{active: true, dragging: true, start: point{0, 0}, end: point{4, 0}}
	u.selection.remember(u.frame)
	u.appMouseDown = true
	u.resizeUntil = time.Now().Add(time.Second)
	f := splitFrame()
	f.Focused = 2
	f.Panes = f.Panes[1:]
	f.Panes[0].X = 0
	f.Panes[0].Width = 21
	u.applyFrame(f)
	if u.focused != 2 || u.selection.active || u.appMouseDown || !u.resizeUntil.IsZero() {
		t.Fatal("closed pane left active input state")
	}
	if _, ok := u.selections[1]; ok {
		t.Fatal("closed pane retained cached selection")
	}
}

func TestLastPaneExitReturnsToPickerAndInvalidatesReader(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	u := &ui{conn: &connection{Conn: a}, name: "api", previous: "api", generation: 4, pending: 99, focused: 1,
		workspace: splitFrame(), frame: splitFrame().Panes[0].Terminal, selection: selection{active: true}}
	u.endSession("api")
	if !u.picker || u.conn != nil || u.generation != 5 || u.pending != 0 || u.frame != nil || u.workspace != nil || u.name != "" || u.previous != "" || u.selection.active {
		t.Fatal("last pane did not clear workspace and open picker")
	}
	if u.message != "Session api closed" {
		t.Fatalf("normal exit reported as failure: %q", u.message)
	}
}

func TestPaneRenderingMouseFocusAndSelectionIsolation(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(21, 8)
	u := &ui{screen: screen}
	u.applyFrame(splitFrame())
	u.draw()
	text, _, _ := screen.Get(11, paneOriginY)
	if text != "r" {
		t.Fatalf("right pane not offset: %q", text)
	}
	text, _, _ = screen.Get(10, paneOriginY+1)
	if text != "┃" {
		t.Fatalf("missing divider: %q", text)
	}
	u.mouse(tcell.NewEventMouse(11, paneOriginY, tcell.Button1, tcell.ModNone))
	u.mouse(tcell.NewEventMouse(15, paneOriginY, tcell.Button1, tcell.ModNone))
	if u.focused != 2 || u.selection.text() != "right" {
		t.Fatalf("right-pane selection: %d %q", u.focused, u.selection.text())
	}
	// Do not release-copy to the real clipboard during this simulation.
	u.selection.dragging = false
	u.activatePane(1)
	if u.selection.active {
		t.Fatal("selection leaked to another pane")
	}
	u.activatePane(2)
	if !u.selection.active || u.selection.text() != "right" {
		t.Fatal("switching panes lost selection")
	}
	u.draw()
	x, y, _ := screen.GetCursor()
	if x != 14 || y != paneOriginY {
		t.Fatalf("cursor not translated: %d,%d", x, y)
	}
	resized := splitFrame()
	resized.Focused = 2
	resized.Panes[1].Width = 9
	u.applyFrame(resized)
	if u.selection.active {
		t.Fatal("resized pane retained a stale selection")
	}
}

func TestPaneBindingsAndInputQueuedBehindSplit(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	u := &ui{screen: screen, conn: &connection{Conn: a, enc: json.NewEncoder(a)}, focused: 1}
	requests := make(chan protocol.Request, 20)
	go func() {
		d := json.NewDecoder(b)
		for {
			var r protocol.Request
			if d.Decode(&r) != nil {
				return
			}
			requests <- r
		}
	}()
	for _, tc := range []struct {
		key           rune
		op, direction string
	}{{'|', "split", "horizontal"}, {'-', "split", "vertical"}, {'h', "resize-pane", "left"}, {'j', "resize-pane", "down"}, {'k', "resize-pane", "up"}, {'l', "resize-pane", "right"}, {'m', "zoom", ""}} {
		u.prefix = true
		u.pending = 0
		u.key(tcell.NewEventKey(tcell.KeyRune, tc.key, tcell.ModNone))
		select {
		case r := <-requests:
			if r.Op != tc.op || r.Direction != tc.direction || r.PaneID != 1 || r.Seq == 0 {
				t.Fatalf("binding %c: %+v", tc.key, r)
			}
		case <-time.After(time.Second):
			t.Fatal("no request")
		}
	}
	// Before the split/focus acknowledgement, input follows the server's newly
	// selected pane rather than being explicitly misrouted to the old pane ID.
	u.key(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	select {
	case r := <-requests:
		if r.Op != "key" || r.PaneID != 0 {
			t.Fatalf("queued input: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no input")
	}
}
