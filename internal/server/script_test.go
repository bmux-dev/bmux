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
	focused := s.focused
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
	focused = s.focused
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
