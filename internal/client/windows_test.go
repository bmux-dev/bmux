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

func windowUI(t *testing.T) (*ui, chan protocol.Request) {
	t.Helper()
	u, requests := sidebarUI(t)
	u.name = "api"
	f := *u.workspace
	f.Windows = []protocol.Window{{Name: "edit", Active: true, Panes: []int{1, 2}}, {Title: "make test", Panes: []int{3}}}
	u.applyFrame(&f)
	// The sidebar's list says pane 3, in the background window, is blocked;
	// nothing in window 1 waits on you.
	u.sessions[1].Activity[0].Attention = false
	u.sessions[1].Activity = append(u.sessions[1].Activity, protocol.PaneActivity{Pane: 3, Agent: "pi", State: protocol.AgentBlocked})
	return u, requests
}

func prefixed(u *ui, key *tcell.EventKey) {
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	u.key(key)
}

func TestWindowTabsShowWindowsAndSelectOnClickOrNumber(t *testing.T) {
	u, requests := windowUI(t)
	u.draw()
	top := pickerScreenLine(u, 0)
	if !strings.Contains(top, " 1 edit ") || !strings.Contains(top, " 2 make test! ") {
		t.Fatalf("top bar: %q", top)
	}
	if len(u.windowTabs) != 2 {
		t.Fatalf("tabs %+v", u.windowTabs)
	}
	if _, style, _ := u.screen.Get(u.windowTabs[0].start+1, 0); func() bool { _, bg, _ := style.Decompose(); return bg != accentColor }() {
		t.Fatal("active window is not in the accent")
	}
	u.mouse(tcell.NewEventMouse(u.windowTabs[1].start+1, 0, tcell.Button1, tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "select-window" || r.Window != 2 {
		t.Fatalf("tab click: %+v", r)
	}
	// The active tab and the bar around the tabs do nothing.
	u.mouse(tcell.NewEventMouse(u.windowTabs[0].start+1, 0, tcell.Button1, tcell.ModNone))
	u.mouse(tcell.NewEventMouse(u.windowTabs[1].end+2, 0, tcell.Button1, tcell.ModNone))
	noRequest(t, requests)

	for _, tc := range []struct {
		key  *tcell.EventKey
		want protocol.Request
	}{
		{runeKey('2'), protocol.Request{Op: "select-window", Window: 2}},
		{runeKey(']'), protocol.Request{Op: "select-window", Direction: "next"}},
		{runeKey('['), protocol.Request{Op: "select-window", Direction: "previous"}},
		{tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), protocol.Request{Op: "select-window", Direction: "last"}},
		{runeKey('c'), protocol.Request{Op: "new-window"}},
	} {
		prefixed(u, tc.key)
		r := nextRequest(t, requests)
		if r.Op != tc.want.Op || r.Window != tc.want.Window || r.Direction != tc.want.Direction {
			t.Fatalf("Ctrl-a %s sent %+v, want %+v", keyName(tc.key), r, tc.want)
		}
	}
}

func TestRenameWindowPromptEditsOnlyTheName(t *testing.T) {
	u, requests := windowUI(t)
	prefixed(u, runeKey(','))
	if u.prompt != "rename-window" || u.input != "edit" {
		t.Fatalf("prompt %q input %q", u.prompt, u.input)
	}
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Rename window 1") || !strings.HasSuffix(strings.TrimRight(footer, " "), ": edit") {
		t.Fatalf("footer %q", footer)
	}
	// Keys and pastes edit the name and never reach the pane.
	u.key(tcell.NewEventKey(tcell.KeyCtrlU, 0, tcell.ModCtrl))
	u.key(runeKey('b'))
	u.handleRawPaste(&rawPasteEvent{Text: "uild\x07\n"})
	u.key(runeKey('x'))
	u.key(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	noRequest(t, requests)
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "rename-window" || r.Window != 1 || r.NewName != "build" {
		t.Fatalf("rename: %+v", r)
	}
	if u.prompt != "" {
		t.Fatal("prompt stayed open")
	}
	prefixed(u, runeKey(','))
	u.key(runeKey('z'))
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.prompt != "" {
		t.Fatal("Esc kept the prompt")
	}
	noRequest(t, requests)
}

func TestCloseWindowAsksWhenAProgramRuns(t *testing.T) {
	dir, err := os.MkdirTemp("", "hv-win-")
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
	asked := make(chan protocol.Request, 4)
	command := make(chan string, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			_ = json.NewDecoder(c).Decode(&r)
			asked <- r
			_ = json.NewEncoder(c).Encode(protocol.Response{Version: protocol.Version, Command: <-command})
			c.Close()
		}
	}()
	u, requests := windowUI(t)
	u.socket = socket

	command <- "vim"
	prefixed(u, runeKey('&'))
	if r := <-asked; r.Op != "pane-command" || r.Kind != "window" || r.Window != 1 || r.Name != "api" {
		t.Fatalf("asked %+v", r)
	}
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Close window 1, which runs vim?") {
		t.Fatalf("footer %q", footer)
	}
	u.key(runeKey('n'))
	noRequest(t, requests)

	command <- "vim"
	prefixed(u, runeKey('&'))
	<-asked
	u.key(runeKey('y'))
	if r := nextRequest(t, requests); r.Op != "close-window" || r.Window != 1 {
		t.Fatalf("close: %+v", r)
	}
	// Panes all at their prompt close without asking.
	command <- ""
	prefixed(u, runeKey('&'))
	<-asked
	if r := nextRequest(t, requests); r.Op != "close-window" || u.closeWindow != 0 {
		t.Fatalf("idle close: %+v", r)
	}
}
