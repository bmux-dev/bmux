package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
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
		{strings.Repeat("─", u.sidebarCols()-1), ""}, {" AGENTS 3", ""},
		{" ● claude", "waiting"}, {"   api", ""},
		{" ● codex", "working"}, {"   api", ""},
		{" ● pi", "blocked"}, {"   db", ""},
	}
	for i, row := range want {
		line := string([]rune(pickerScreenLine(u, paneOriginY+i))[:u.sidebarCols()-1])
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
	if text, _, _ := u.screen.Get(u.sidebarCols()-1, paneOriginY); text != "│" {
		t.Fatalf("sidebar border = %q", text)
	}
	if text, _, _ := u.screen.Get(u.sidebarCols(), paneOriginY); text != "l" {
		t.Fatalf("pane not drawn right of the sidebar: %q", text)
	}
	if x, y, visible := u.screen.(tcell.SimulationScreen).GetCursor(); !visible || x != u.sidebarCols()+1 || y != paneOriginY {
		t.Fatalf("cursor = %d,%d visible=%v", x, y, visible)
	}

	// Mouse coordinates are pane-relative, not screen columns.
	u.frame.Mouse = true
	u.mouse(tcell.NewEventMouse(u.sidebarCols()+3, paneOriginY, tcell.Button1, tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "mouse" || r.X != 3 || r.Y != 0 {
		t.Fatalf("pane click: %+v", r)
	}
	u.mouse(tcell.NewEventMouse(u.sidebarCols()+3, paneOriginY, tcell.ButtonNone, tcell.ModNone))
	_ = nextRequest(t, requests)

	// Hiding the sidebar gives its columns back to the panes.
	u.prefix = true
	u.key(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "resize" || r.Width != 100 || r.Height != 18 {
		t.Fatalf("hide resize: %+v", r)
	}
	u.prefix = true
	u.key(tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "resize" || r.Width != 100-u.sidebarCols() {
		t.Fatalf("show resize: %+v", r)
	}
	u.screen.(tcell.SimulationScreen).SetSize(u.sidebarMinScreen()-1, 20)
	if u.paneOriginX(u.sidebarMinScreen()-1) != 0 {
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

func TestSidebarClickSelectsThenOpensOnDoubleClick(t *testing.T) {
	u, requests := sidebarUI(t)
	click := func(y int) { u.mouse(tcell.NewEventMouse(3, y, tcell.Button1, tcell.ModNone)) }

	// A first click on claude's agent line selects it and focuses the
	// sidebar, so d and other sidebar keys work right away, but does not
	// switch sessions.
	click(paneOriginY + 7)
	noRequest(t, requests)
	if !u.sidebarFocus || u.sidebarSelected != (sidebarTarget{"api", 2}) {
		t.Fatalf("click did not select and focus: focus %v selected %+v", u.sidebarFocus, u.sidebarSelected)
	}
	// A second click on the same row within the double-click window opens it.
	click(paneOriginY + 7)
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("double-click: %+v", r)
	}
	if u.sidebarFocus {
		t.Fatal("opening a row kept sidebar focus")
	}

	click(paneOriginY)     // Sessions heading
	click(paneOriginY + 4) // divider
	click(paneOriginY + 5) // Agents heading
	u.mouse(tcell.NewEventMouse(3, paneOriginY+3, tcell.WheelDown, tcell.ModNone))
	noRequest(t, requests)

	// A single click on a different session row only selects it.
	click(paneOriginY + 1)
	noRequest(t, requests)
	if got := u.sidebarSelected; got != (sidebarTarget{"api", 0}) {
		t.Fatalf("session row click selection: %+v", got)
	}
	// Double-clicking it opens that session's first pane waiting on you.
	click(paneOriginY + 1)
	if r := nextRequest(t, requests); r.Op != "select-pane" || r.PaneID != 2 {
		t.Fatalf("session row double-click: %+v", r)
	}
	noRequest(t, requests)
}

func TestSidebarDeleteAsksAndOnlyYTerminates(t *testing.T) {
	// A short private socket path avoids Darwin's sockaddr_un path limit.
	dir, err := os.MkdirTemp("", "hv-side-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "rpc")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	rpcs := make(chan protocol.Request, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			_ = json.NewDecoder(c).Decode(&r)
			rpcs <- r
			_ = json.NewEncoder(c).Encode(protocol.Response{Version: protocol.Version})
			c.Close()
		}
	}()
	u, requests := sidebarUI(t)
	u.socket = socket
	u.focusSidebar()
	u.key(runeKey('j')) // api → web, in creation order
	web := u.sessions[0]
	ask := func() {
		t.Helper()
		u.key(runeKey('d'))
		if u.prompt != "delete" || u.deleteTarget.Name != "web" || !u.deleteTarget.Created.Equal(web.Created) {
			t.Fatalf("d on a session row did not ask about it: prompt %q target %+v", u.prompt, u.deleteTarget)
		}
	}
	ask()
	u.draw()
	if text := pickerScreenText(u); !strings.Contains(text, "Delete \"web\"?") || !strings.Contains(text, "Yes (y)") || !strings.Contains(pickerScreenLine(u, 19), "y yes") {
		t.Fatalf("confirmation not shown over the attached view:\n%s", text)
	}

	// Anything but a plain y keeps the session and reaches neither the
	// application nor the server; the sidebar keeps focus and its selection.
	for _, cancel := range []func(){
		func() { u.key(runeKey('n')) },
		func() { u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)) },
		func() { u.key(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModCtrl)) },
		func() { u.key(runeKey('j')) },
		func() { u.handleRawPaste(&rawPasteEvent{Text: "y"}) },
		func() { u.mouse(tcell.NewEventMouse(3, paneOriginY+1, tcell.Button1, tcell.ModNone)) },
	} {
		cancel()
		if u.prompt != "" || u.deleteBusy || !u.sidebarFocus || u.sidebarSelected != (sidebarTarget{"web", 0}) {
			t.Fatalf("cancel left prompt %q busy %v focus %v selection %+v", u.prompt, u.deleteBusy, u.sidebarFocus, u.sidebarSelected)
		}
		ask()
	}
	noRequest(t, requests)
	select {
	case r := <-rpcs:
		t.Fatalf("termination sent before confirmation: %+v", r)
	default:
	}

	u.key(runeKey('y'))
	if u.prompt != "" || !u.deleteBusy {
		t.Fatal("y did not start the termination")
	}
	select {
	case r := <-rpcs:
		if r.Op != "kill" || r.Name != "web" || r.Generation != protocol.SessionGeneration(web.Created) {
			t.Fatalf("wrong termination: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no termination")
	}
	select {
	case result := <-u.deletions:
		u.finishDelete(result)
	case <-time.After(time.Second):
		t.Fatal("no termination result")
	}
	if u.message != "Deleted session web" {
		t.Fatalf("message %q", u.message)
	}

	// An agent row stands for one pane: d never deletes its whole session.
	u.sidebarSelected = sidebarTarget{"api", 2}
	u.key(runeKey('d'))
	if u.prompt != "" || u.message != "Select a session row to delete it" {
		t.Fatalf("d on an agent row: prompt %q message %q", u.prompt, u.message)
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
