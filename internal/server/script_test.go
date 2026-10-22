package server

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

func awaitCapture(t *testing.T, srv *Server, pane int, want string) protocol.Response {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := srv.request(protocol.Request{Op: "capture-pane", Name: "view", PaneID: pane})
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		if strings.Contains(r.Text, want) {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %d never showed %q:\n%s", pane, want, r.Text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSendInputTargetsOnePaneWithoutResizingOrFocusing(t *testing.T) {
	srv, s := viewportSession(t) // 100x30
	if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	focused := s.focusedLocked()
	s.mu.Unlock()
	if focused != 2 {
		t.Fatalf("split focused pane %d", focused)
	}
	send := protocol.Request{Op: "send-input", Name: "view", PaneID: 1, Input: []protocol.Input{
		{Paste: "echo LEFT_$((2+3))"}, {Code: uv.KeyEnter},
	}}
	if r := srv.request(send); r.Error != "" {
		t.Fatal(r.Error)
	}
	r := awaitCapture(t, srv, 1, "LEFT_5")
	if r.Pane == nil || r.Pane.Pane != 1 {
		t.Fatalf("capture answered for %+v", r.Pane)
	}
	if focusedText := srv.request(protocol.Request{Op: "capture-pane", Name: "view"}).Text; strings.Contains(focusedText, "LEFT_5") {
		t.Fatal("input for pane 1 reached the focused pane")
	}
	s.mu.Lock()
	focused = s.focusedLocked()
	s.mu.Unlock()
	if focused != 2 {
		t.Fatal("send-input moved focus")
	}
	assertSessionSize(t, s, 100, 30)

	// Input never falls back to the focused pane.
	send.PaneID = 0
	if r := srv.request(send); r.Error == "" {
		t.Fatal("send-input without a pane was accepted")
	}
}

func TestSendInputIfSeqRefusesStaleStateAndWritesNothing(t *testing.T) {
	srv, s := viewportSession(t)
	if err := s.reportAgent(1, protocol.AgentBlocked, "omp", nil); err != nil {
		t.Fatal(err)
	}
	seq := srv.request(protocol.Request{Op: "capture-pane", Name: "view", PaneID: 1}).Pane.Seq
	if err := s.reportAgent(1, protocol.AgentWorking, "", nil); err != nil {
		t.Fatal(err)
	}
	stale := srv.request(protocol.Request{Op: "send-input", Name: "view", PaneID: 1, IfSeq: seq,
		Input: []protocol.Input{{Paste: "echo STALE_$((1+1))"}, {Code: uv.KeyEnter}}})
	if !strings.Contains(stale.Error, "stale") || stale.Pane == nil || stale.Pane.Seq == seq {
		t.Fatalf("stale send: %+v", stale)
	}
	// Input is ordered, so anything the stale send wrote would show first.
	if r := srv.request(protocol.Request{Op: "send-input", Name: "view", PaneID: 1, IfSeq: stale.Pane.Seq,
		Input: []protocol.Input{{Paste: "echo FRESH_$((1+1))"}, {Code: uv.KeyEnter}}}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if text := awaitCapture(t, srv, 1, "FRESH_2").Text; strings.Contains(text, "STALE") {
		t.Fatalf("a stale send was typed:\n%s", text)
	}
}

func TestTargetsResolveToPanesAndWindows(t *testing.T) {
	s := windowSession(t)
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // window 1: panes 1, 2 (focus 2)
	mustInput(t, s, protocol.Request{Op: "new-window"})                     // window 2: pane 3
	mustInput(t, s, protocol.Request{Op: "split", Direction: "vertical"})   // window 2: panes 3, 4 (focus 4)
	mustInput(t, s, protocol.Request{Op: "select-pane", PaneID: 3})
	resolve := func(target paneTarget) (int, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.resolveLocked(target)
	}
	for _, c := range []struct {
		name   string
		target paneTarget
		want   int
	}{
		{"session: the active window's focused pane", paneTarget{}, 3},
		{"pane number, in a background window", paneTarget{pane: 2}, 2},
		{"window: its focused pane", paneTarget{window: 1}, 2},
		{"window.index", paneTarget{window: 1, index: 1}, 1},
		{"window.index in the active window", paneTarget{window: 2, index: 2}, 4},
	} {
		if got, err := resolve(c.target); err != nil || got != c.want {
			t.Errorf("%s: pane %d %v, want %d", c.name, got, err, c.want)
		}
	}
	for _, c := range []struct {
		target paneTarget
		want   string
	}{
		{paneTarget{pane: 9}, "no pane 9 in session windows"},
		{paneTarget{window: 3}, "no window 3 in session windows, which has 2"},
		{paneTarget{window: 2, index: 3}, "window 2 of session windows has 2 panes, not 3"},
		{paneTarget{index: 1}, "needs a window"},
	} {
		if _, err := resolve(c.target); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.target, err, c.want)
		}
	}
	// tmux's session:window, written for a pane number no pane has. Closing
	// a window ends its shells; their panes leave once those exit.
	mustInput(t, s, protocol.Request{Op: "close-window", Window: 1})
	mustInput(t, s, protocol.Request{Op: "new-window"}) // windows: panes 3, 4 | pane 5
	closed := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.windows) == 2 && s.panes[1] == nil && s.panes[2] == nil
	}
	for deadline := time.Now().Add(5 * time.Second); !closed() && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	_, err := resolve(paneTarget{pane: 2})
	if err == nil || !strings.Contains(err.Error(), "for window 2, write windows:2.") {
		t.Fatalf("api:N for a window: %v", err)
	}

	// Agent activity places each pane, for wait-agent window targets.
	if err := s.reportAgent(4, protocol.AgentIdle, "", nil); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	activity := s.activityLocked()
	s.mu.Unlock()
	if len(activity) != 1 || activity[0].Pane != 4 || activity[0].Window != 1 || activity[0].Index != 2 {
		t.Fatalf("activity: %+v", activity)
	}

	// Sending needs a pane, not just a window.
	if _, err := s.sendInput(paneTarget{window: 1}, 0, []protocol.Input{{Paste: "x"}}); err == nil {
		t.Fatal("send-input to a window was accepted")
	}
}
