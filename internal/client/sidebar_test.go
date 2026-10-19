package client

import (
	"encoding/json"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func sidebarUI(t *testing.T) (*ui, chan protocol.Request) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(100, 20)
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	requests := make(chan protocol.Request, 32)
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
	now := time.Now()
	u := &ui{screen: screen, conn: &connection{Conn: a, enc: json.NewEncoder(a)}, sessions: []protocol.Session{
		// Recency order from the server; the sidebar shows creation order.
		{Name: "web", Created: now.Add(time.Minute), Panes: 1},
		{Name: "api", Created: now, Panes: 2, Activity: []protocol.PaneActivity{
			{Pane: 2, Agent: "claude", State: protocol.AgentIdle, Attention: true},
			{Pane: 3, Agent: "codex", State: protocol.AgentWorking},
		}},
		{Name: "db", Created: now.Add(2 * time.Minute), Panes: 1, Activity: []protocol.PaneActivity{{Pane: 1, Agent: "pi", State: protocol.AgentBlocked}}},
	}}
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Edge: 7, EdgeDirection: "left", Panes: []protocol.PaneFrame{
		{ID: 1, Width: 37, Height: 8, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("left")}, CursorVisible: true, CursorX: 1}},
		{ID: 2, X: 38, Width: 38, Height: 8, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("right")}}},
	}})
	return u, requests
}

func nextRequest(t *testing.T, requests chan protocol.Request) protocol.Request {
	t.Helper()
	select {
	case r := <-requests:
		return r
	case <-time.After(time.Second):
		t.Fatal("no request")
		return protocol.Request{}
	}
}

func noRequest(t *testing.T, requests chan protocol.Request) {
	t.Helper()
	select {
	case r := <-requests:
		t.Fatalf("unexpected request %+v", r)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSidebarLayoutOffsetsPanesAndListsAgents(t *testing.T) {
	u, requests := sidebarUI(t)
	if u.sidebarFocus {
		t.Fatal("an edge from before the attachment moved focus into the sidebar")
	}
	u.draw()
	// Sessions in creation order, a divider, then every agent across sessions
	// as two-line entries: icon, agent and state, then the session name.
	want := []struct{ text, right string }{
		{" SESSIONS", ""}, {" ● api", ""}, {"   web", ""}, {" ● db", ""},
		{strings.Repeat("─", sidebarWidth-1), ""}, {" AGENTS 3", ""},
		{" ● claude", "waiting"}, {"   api", ""},
		{" ● codex", "working"}, {"   api", ""},
		{" ● pi", "blocked"}, {"   db", ""},
	}
	for i, row := range want {
		line := string([]rune(pickerScreenLine(u, paneOriginY+i))[:sidebarWidth-1])
		if !strings.HasPrefix(line, row.text) || strings.TrimSpace(strings.TrimPrefix(line, row.text)) != row.right {
			t.Fatalf("sidebar row %d = %q, want %q … %q", i, line, row.text, row.right)
		}
	}
	// herdr's colors: done teal, working yellow, blocked red; sessions roll up
	// the most urgent state (api: done beats working).
	for _, tc := range []struct {
		row   int
		style tcell.Style
	}{{1, sidebarDoneStyle}, {3, sidebarBlockedStyle}, {6, sidebarDoneStyle}, {8, sidebarWorkingStyle}, {10, sidebarBlockedStyle}} {
		if _, style, _ := u.screen.Get(1, paneOriginY+tc.row); style != tc.style {
			t.Fatalf("icon style on row %d = %v, want %v", tc.row, style, tc.style)
		}
	}
	if _, style, _ := u.screen.Get(3, paneOriginY+1); style != sidebarAccentStyle {
		t.Fatal("attached session is not highlighted")
	}
	if _, style, _ := u.screen.Get(0, paneOriginY+12); style != sidebarBaseStyle {
		t.Fatal("sidebar does not use its own background")
	}
	if text, _, _ := u.screen.Get(sidebarWidth-1, paneOriginY); text != "│" {
		t.Fatalf("sidebar border = %q", text)
	}
	if text, _, _ := u.screen.Get(sidebarWidth, paneOriginY); text != "l" {
		t.Fatalf("pane not drawn right of the sidebar: %q", text)
	}
	if x, y, visible := u.screen.(tcell.SimulationScreen).GetCursor(); !visible || x != sidebarWidth+1 || y != paneOriginY {
		t.Fatalf("cursor = %d,%d visible=%v", x, y, visible)
	}

	// Mouse coordinates are pane-relative, not screen columns.
	u.frame.Mouse = true
	u.mouse(tcell.NewEventMouse(sidebarWidth+3, paneOriginY, tcell.Button1, tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "mouse" || r.X != 3 || r.Y != 0 {
		t.Fatalf("pane click: %+v", r)
	}
	u.mouse(tcell.NewEventMouse(sidebarWidth+3, paneOriginY, tcell.ButtonNone, tcell.ModNone))
	_ = nextRequest(t, requests)

	// Hiding the sidebar gives its columns back to the panes.
	u.prefix = true
	u.key(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "resize" || r.Width != 100 || r.Height != 18 {
		t.Fatalf("hide resize: %+v", r)
	}
	u.prefix = true
	u.key(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "resize" || r.Width != 100-sidebarWidth {
		t.Fatalf("show resize: %+v", r)
	}
	u.screen.(tcell.SimulationScreen).SetSize(sidebarMinScreenWidth-1, 20)
	if u.paneOriginX(sidebarMinScreenWidth-1) != 0 {
		t.Fatal("narrow terminal kept the sidebar")
	}
}

func TestLeftEdgeFocusesSidebarAndKeysPickRows(t *testing.T) {
	u, requests := sidebarUI(t)
	edge := func(n uint64, direction string) {
		f := *u.workspace
		f.Edge, f.EdgeDirection = n, direction
		u.applyFrame(&f)
	}
	edge(8, "up")
	if u.sidebarFocus {
		t.Fatal("an upward edge focused the sidebar")
	}
	edge(9, "left")
	if !u.sidebarFocus {
		t.Fatal("running off the left edge did not focus the sidebar")
	}
	edge(9, "left")
	u.draw()
	if _, _, visible := u.screen.(tcell.SimulationScreen).GetCursor(); visible {
		t.Fatal("application cursor shown while the sidebar has focus")
	}
	if !strings.Contains(pickerScreenLine(u, 19), "Sidebar") {
		t.Fatal("footer does not describe sidebar keys")
	}

	// Keys go to the sidebar, never to the application.
	u.key(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	u.key(tcell.NewEventKey(tcell.KeyCtrlH, 0, tcell.ModCtrl))
	noRequest(t, requests)
	if got := u.sidebarSelected; got != (sidebarTarget{"api", 0}) {
		t.Fatalf("initial selection %+v", got)
	}
	// j/k walk sessions, then cross the divider into agents.
	for _, key := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyCtrlJ, 0, tcell.ModCtrl),
		tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone),
	} {
		u.key(key)
	}
	if got := u.sidebarSelected; got != (sidebarTarget{"api", 2}) {
		t.Fatalf("selection after moving into agents: %+v", got)
	}
	u.draw()
	if _, style, _ := u.screen.Get(5, paneOriginY+7); style != sidebarSelectedStyle {
		t.Fatal("both lines of the selected agent are not highlighted")
	}
	u.key(tcell.NewEventKey(tcell.KeyCtrlL, 0, tcell.ModCtrl))
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("opening an agent row: %+v", r)
	}
	if u.sidebarFocus {
		t.Fatal("opening a row kept sidebar focus")
	}

	// A session row jumps to the first pane waiting on you.
	u.pending = 0
	u.focusSidebar()
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("opening a session row: %+v", r)
	}

	u.focusSidebar()
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.sidebarFocus {
		t.Fatal("Esc kept sidebar focus")
	}
	u.focusSidebar()
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	if u.sidebarFocus || !u.prefix {
		t.Fatal("Ctrl-a in the sidebar did not start a prefix command")
	}
	noRequest(t, requests)
}

func TestSidebarClickOpensRow(t *testing.T) {
	u, requests := sidebarUI(t)
	u.mouse(tcell.NewEventMouse(3, paneOriginY+7, tcell.Button1, tcell.ModNone)) // claude's session line
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("sidebar click: %+v", r)
	}
	u.mouse(tcell.NewEventMouse(3, paneOriginY, tcell.Button1, tcell.ModNone))   // Sessions heading
	u.mouse(tcell.NewEventMouse(3, paneOriginY+4, tcell.Button1, tcell.ModNone)) // divider
	u.mouse(tcell.NewEventMouse(3, paneOriginY+5, tcell.Button1, tcell.ModNone)) // Agents heading
	u.mouse(tcell.NewEventMouse(3, paneOriginY+3, tcell.WheelDown, tcell.ModNone))
	noRequest(t, requests)
	// A session row opens that session's first pane waiting on you.
	u.mouse(tcell.NewEventMouse(3, paneOriginY+1, tcell.Button1, tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("session row click: %+v", r)
	}
	noRequest(t, requests)
}

func TestAgentAlertsSoundOncePerTransition(t *testing.T) {
	var played []bool
	u := &ui{sound: func(blocked bool) { played = append(played, blocked) }}
	list := func(activity ...protocol.PaneActivity) []protocol.Session {
		return []protocol.Session{{Name: "api", Activity: activity}}
	}
	working := protocol.PaneActivity{Pane: 1, Agent: "codex", State: protocol.AgentWorking}
	ready := protocol.PaneActivity{Pane: 1, Agent: "codex", State: protocol.AgentIdle, Attention: true}
	blocked := protocol.PaneActivity{Pane: 1, Agent: "codex", State: protocol.AgentBlocked, Attention: true}
	bell := protocol.PaneActivity{Pane: 2, Attention: true}

	u.updateSessions(list(ready)) // baseline: an agent already waiting at startup
	u.updateSessions(list(ready))
	u.updateSessions(list(working))
	u.updateSessions(list(ready))
	u.updateSessions(list(ready))
	u.updateSessions(list(ready, bell)) // a plain terminal bell marks but stays silent
	u.updateSessions(list(blocked))
	u.updateSessions(list(blocked))
	if want := []bool{false, true}; !reflect.DeepEqual(played, want) {
		t.Fatalf("sounds = %v, want %v (ready, then blocked)", played, want)
	}
}
