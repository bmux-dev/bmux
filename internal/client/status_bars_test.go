package client

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestStatusBarsPersistInPickerAndAttachedView(t *testing.T) {
	u := pickerUI(t)
	u.draw()
	if !strings.Contains(pickerScreenLine(u, 0), "") || !strings.Contains(pickerScreenLine(u, 23), "attach") {
		t.Fatal("picker lost its top powerline or bottom controls")
	}
	u.picker = false
	u.sidebarHidden = true // the sidebar's own layout is covered in sidebar_test.go
	rows := make([][]protocol.Cell, 24)
	for y := range rows {
		rows[y] = cells(strings.Repeat("X", 100))
	}
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 100, Height: 24, Terminal: &protocol.TerminalFrame{Rows: rows, CursorVisible: true, CursorX: 4, CursorY: 21}}}})
	u.draw()
	top, bottom := pickerScreenLine(u, 0), pickerScreenLine(u, 23)
	if !strings.Contains(top, "") || !strings.Contains(top, "api") || !strings.Contains(top, "pane 1/1") {
		t.Fatalf("attached top bar missing: %s", top)
	}
	if !strings.Contains(bottom, "Ctrl-a s sessions") {
		t.Fatalf("attached bottom bar missing: %s", bottom)
	}
	for y := 1; y < 23; y++ {
		if line := pickerScreenLine(u, y); line != strings.Repeat("X", 100) {
			t.Fatalf("content changed at screen row %d", y)
		}
	}
	x, y, visible := u.screen.(tcell.SimulationScreen).GetCursor()
	if !visible || x != 4 || y != 22 {
		t.Fatal("cursor did not account for top bar")
	}
	u.frame.CursorY = 22
	u.draw()
	if _, _, visible = u.screen.(tcell.SimulationScreen).GetCursor(); visible {
		t.Fatal("stale oversized frame placed cursor on bottom bar")
	}
}

func TestTinyAttachedViewReservesChromeAndTerminalSizing(t *testing.T) {
	u := pickerUI(t)
	u.picker = false
	u.applyFrame(splitFrame())
	for _, height := range []int{1, 2, 3, 24} {
		u.screen.(tcell.SimulationScreen).SetSize(100, height)
		u.draw()
		if !strings.Contains(pickerScreenLine(u, 0), "") {
			t.Fatalf("top bar absent at height %d", height)
		}
		if height > 1 && !strings.Contains(pickerScreenLine(u, height-1), "sessions") {
			t.Fatalf("bottom bar absent at height %d", height)
		}
		if got := paneViewportHeight(height); got != max(1, height-2) {
			t.Fatalf("PTY height = %d for screen height %d", got, height)
		}
		if height <= 2 {
			if _, _, visible := u.screen.(tcell.SimulationScreen).GetCursor(); visible {
				t.Fatal("tiny view placed app cursor in chrome")
			}
		}
	}
}

func TestStatusBarMouseEventsStayOutsideApplication(t *testing.T) {
	s := focusTestScreen(t, 20, 5)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	u := &ui{screen: s, conn: &connection{Conn: a, enc: json.NewEncoder(a)}}
	rows := [][]protocol.Cell{cells("first"), cells("second"), cells("third"), cells("stale"), cells("stale")}
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 20, Height: 5, Terminal: &protocol.TerminalFrame{Rows: rows, Mouse: true}}}})
	requests := make(chan protocol.Request, 8)
	go func() {
		dec := json.NewDecoder(b)
		for {
			var r protocol.Request
			if dec.Decode(&r) != nil {
				return
			}
			requests <- r
		}
	}()
	u.mouse(tcell.NewEventMouse(2, 0, tcell.Button1, tcell.ModNone))
	u.mouse(tcell.NewEventMouse(2, 4, tcell.WheelDown, tcell.ModNone))
	u.mouse(tcell.NewEventMouse(2, paneOriginY+1, tcell.Button1, tcell.ModNone))
	read := func() protocol.Request {
		t.Helper()
		select {
		case r := <-requests:
			return r
		case <-time.After(time.Second):
			t.Fatal("no application mouse event")
			return protocol.Request{}
		}
	}
	if r := read(); r.Op != "mouse" || r.X != 2 || r.Y != 1 || r.Kind != "click" {
		t.Fatalf("chrome forwarded input or app coordinates changed: %+v", r)
	}
	u.mouse(tcell.NewEventMouse(2, 0, tcell.ButtonNone, tcell.ModNone))
	if r := read(); r.Kind != "release" || r.Y != 0 {
		t.Fatalf("release on top bar was not clamped: %+v", r)
	}
	if u.appMouseDown || u.picker {
		t.Fatal("chrome mouse release left transient state")
	}
	u.mouse(tcell.NewEventMouse(2, paneOriginY+1, tcell.Button1, tcell.ModNone))
	_ = read()
	u.mouse(tcell.NewEventMouse(2, 4, tcell.ButtonNone, tcell.ModNone))
	if r := read(); r.Kind != "release" || r.Y != 2 {
		t.Fatalf("release on bottom bar was not clamped to visible pane: %+v", r)
	}
	select {
	case r := <-requests:
		t.Fatalf("extra chrome input: %+v", r)
	default:
	}
}
