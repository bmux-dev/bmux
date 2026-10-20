package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

// viewUI is an attached UI whose focused pane shows rows 100-104 of a history
// that starts at row 0, with the cursor on row 104.
func viewUI(t *testing.T) (*ui, chan protocol.Request) {
	t.Helper()
	u, requests := sidebarUI(t)
	rows := [][]protocol.Cell{cells("one two three"), cells("four five"), cells("six"), cells("seven"), cells("$ ls")}
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 13, Height: 5, Terminal: &protocol.TerminalFrame{Rows: rows, Base: 100, Epoch: 3, CursorX: 2, CursorY: 4, CursorVisible: true}},
	}})
	return u, requests
}

// rpcServer answers one-shot requests with respond and records them.
func rpcServer(t *testing.T, respond func(protocol.Request) protocol.Response) (string, chan protocol.Request) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hv-view-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "rpc")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	got := make(chan protocol.Request, 8)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			_ = json.NewDecoder(c).Decode(&r)
			got <- r
			response := respond(r)
			response.Version = protocol.Version
			_ = json.NewEncoder(c).Encode(response)
			c.Close()
		}
	}()
	return socket, got
}

func TestScrollbackViewMovesScrollsAndKeepsKeysFromThePane(t *testing.T) {
	u, requests := viewUI(t)
	prefixed(u, runeKey('v'))
	if !u.view.active || u.view.cursor != (point{2, 104}) {
		t.Fatalf("view %+v", u.view)
	}
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Scrollback:") {
		t.Fatalf("footer %q", footer)
	}
	// Moving above the viewport scrolls history one row at a time.
	for range 5 {
		u.key(runeKey('k'))
	}
	if u.view.cursor != (point{2, 99}) {
		t.Fatalf("cursor %+v", u.view.cursor)
	}
	if r := nextRequest(t, requests); r.Op != "scroll" || r.Delta != 1 {
		t.Fatalf("scroll %+v", r)
	}
	// Keys the view doesn't use never reach the pane.
	u.key(runeKey('x'))
	u.key(runeKey('z'))
	noRequest(t, requests)
	u.key(runeKey('G'))
	if u.view.cursor.y != 104 {
		t.Fatalf("G went to %d", u.view.cursor.y)
	}
	if r := nextRequest(t, requests); r.Op != "scroll" || r.Delta != -1 {
		t.Fatalf("scroll back down %+v", r)
	}
	// Word and line motions on a row that's on screen.
	u.viewMove(point{0, 100})
	u.key(runeKey('w'))
	if u.view.cursor != (point{4, 100}) {
		t.Fatalf("w: %+v", u.view.cursor)
	}
	u.key(runeKey('$'))
	if u.view.cursor != (point{12, 100}) {
		t.Fatalf("$: %+v", u.view.cursor)
	}
	u.key(runeKey('b'))
	if u.view.cursor != (point{8, 100}) {
		t.Fatalf("b: %+v", u.view.cursor)
	}

	// Esc first ends a selection, then leaves and returns to live output.
	u.key(runeKey('v'))
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if !u.view.active || u.view.mode != "" {
		t.Fatal("first Esc should only end the selection")
	}
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.view.active {
		t.Fatal("second Esc kept the view")
	}
	if r := nextRequest(t, requests); r.Op != "scroll" || r.Delta != 0 {
		t.Fatalf("leaving: %+v", r)
	}
}

func TestScrollbackViewCopiesFromServerHistory(t *testing.T) {
	u, requests := viewUI(t)
	socket, rpcs := rpcServer(t, func(protocol.Request) protocol.Response { return protocol.Response{Text: "two three\nfour"} })
	u.socket = socket
	prefixed(u, runeKey('v'))
	u.viewMove(point{4, 100})
	u.key(runeKey('v'))
	u.key(runeKey('j'))
	u.draw()
	if _, style, _ := u.screen.Get(u.paneOriginX(100)+6, paneOriginY); style == cellStyle(cells("x")[0]) {
		t.Fatal("selection not highlighted")
	}
	u.key(runeKey('y'))
	r := <-rpcs
	if r.Op != "history-text" || r.PaneID != 1 || r.Range == nil {
		t.Fatalf("copy request %+v", r)
	}
	if got := *r.Range; got != (protocol.TextRange{Epoch: 3, FromX: 4, FromY: 100, ToX: 4, ToY: 101, Mode: "char"}) {
		t.Fatalf("range %+v", got)
	}
	if u.view.active || !strings.Contains(u.message, "Clipboard") {
		t.Fatalf("after copy: view %v message %q", u.view.active, u.message)
	}
	if r := nextRequest(t, requests); r.Op != "scroll" || r.Delta != 0 {
		t.Fatalf("copying didn't return to live output: %+v", r)
	}
	// Without a selection, y says how to make one.
	prefixed(u, runeKey('v'))
	u.key(runeKey('y'))
	if !u.view.active || !strings.Contains(u.message, "Nothing selected") {
		t.Fatalf("y without selection: %q", u.message)
	}
	// A frame from another epoch closes the view.
	f := *u.workspace
	terminal := *f.Panes[0].Terminal
	terminal.Epoch = 4
	f.Panes = []protocol.PaneFrame{{ID: 1, Width: 13, Height: 5, Terminal: &terminal}}
	u.applyFrame(&f)
	if u.view.active {
		t.Fatal("an epoch change kept the view")
	}
}
