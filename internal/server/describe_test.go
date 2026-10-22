package server

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestDescribeReportsPanesWindowsAndSessions(t *testing.T) {
	srv, s := viewportSession(t) // 100x30, session "view"
	dir := realDir(t, t.TempDir())
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // window 1: panes 1, 2 (focus 2)
	mustInput(t, s, protocol.Request{Op: "new-window", NewName: "logs"})    // window 2: pane 3
	mustInput(t, s, protocol.Request{Op: "zoom"})
	if r := srv.request(protocol.Request{Op: "send-input", Name: "view", PaneID: 3, Input: []protocol.Input{
		{Paste: "cd " + dir + " && sleep 30"}, {Code: uv.KeyEnter},
	}}); r.Error != "" {
		t.Fatal(r.Error)
	}
	describe := func(r protocol.Request) []protocol.PaneInfo {
		t.Helper()
		r.Op, r.Name = "describe", "view"
		response := srv.request(r)
		if response.Error != "" {
			t.Fatal(response.Error)
		}
		return response.Panes
	}
	// Window 2's pane, once its foreground program is sleep.
	var pane protocol.PaneInfo
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		pane = describe(protocol.Request{Kind: "pane", Window: 2, PaneIndex: 1})[0]
		if pane.Command == "sleep" && pane.Path == dir {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane 3 never ran sleep in %s: %+v", dir, pane)
		}
	}
	want := protocol.PaneInfo{
		Session: "view", SessionID: s.id, SessionPath: pane.SessionPath, SessionWindows: 2,
		Window: 2, WindowName: "logs", WindowActive: true, WindowZoomed: true, WindowPanes: 1,
		Pane: 3, Index: 1, Active: true, Path: dir, Command: "sleep", Title: pane.Title, Width: 100, Height: 30,
	}
	if pane != want {
		t.Fatalf("pane:\n got %+v\nwant %+v", pane, want)
	}

	// Window 1 has two side-by-side panes; pane 2 is its focused one.
	window := describe(protocol.Request{Kind: "window", PaneID: 1})
	if len(window) != 2 || window[0].Pane != 1 || window[1].Pane != 2 || window[0].Active || !window[1].Active ||
		window[0].WindowActive || window[0].WindowPanes != 2 || window[0].Width+window[1].Width != 99 || window[0].Height != 30 {
		t.Fatalf("window 1: %+v", window)
	}
	if session := describe(protocol.Request{Kind: "session"}); len(session) != 3 {
		t.Fatalf("session: %d panes", len(session))
	}
	// An agent shows up with its state.
	if err := s.reportAgent(1, protocol.AgentBlocked, "claude", nil); err != nil {
		t.Fatal(err)
	}
	if p := describe(protocol.Request{Kind: "pane", PaneID: 1})[0]; p.Agent != "claude" || p.State != protocol.AgentBlocked {
		t.Fatalf("agent pane: %+v", p)
	}
	if all := srv.request(protocol.Request{Op: "describe", Kind: "all"}); len(all.Panes) != 3 {
		t.Fatalf("all sessions: %+v", all)
	}
	if r := srv.request(protocol.Request{Op: "describe", Name: "view", Kind: "pane", Window: 5}); !strings.Contains(r.Error, "no window 5") {
		t.Fatalf("bad target: %+v", r)
	}
}

func TestDisplayMessageReachesFrames(t *testing.T) {
	srv, s := viewportSession(t)
	before, revision := s.snapshot(map[int]int{}, map[int]int{}, 0, true)
	if r := srv.request(protocol.Request{Op: "display-message", Name: "view", Text: "build done"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	after, _ := s.snapshot(map[int]int{}, map[int]int{}, revision, false)
	if after == nil || after.Message != "build done" || after.MessageSeq != before.MessageSeq+1 {
		t.Fatalf("frame after display-message: %+v", after)
	}
}
