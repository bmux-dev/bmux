package server

import (
	"github.com/josephferrero/bmux/internal/protocol"
	"strings"
	"testing"
	"time"
)

func TestBlockedPaneDoesNotBlockGlobalManagementOrParallelShutdown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New("/bin/sh")
	defer s.Close()
	dir := t.TempDir()
	for _, name := range []string{"blocked", "other"} {
		if r := s.request(protocol.Request{Op: "create", Name: name, Dir: dir}); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	s.mu.Lock()
	owned := []*pane{s.sessions["blocked"].panes[1], s.sessions["other"].panes[1]}
	s.mu.Unlock()
	defer func() {
		for _, p := range owned {
			_ = p.cmd.Process.Kill()
			p.closeIO()
		}
	}()
	s.mu.Lock()
	blocked := s.sessions["blocked"]
	s.mu.Unlock()
	if err := blocked.input(protocol.Request{Op: "paste", Text: "stty -icanon -echo; printf 'READY\\n'; exec sleep 30\n"}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, blocked.panes[1], func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "READY") })
	if err := blocked.input(protocol.Request{Op: "paste", Text: strings.Repeat("x", 1<<20)}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []protocol.Request{{Op: "ping"}, {Op: "list"}, {Op: "preview", Name: "blocked", Kind: "overview"}, {Op: "kill", Name: "other"}} {
		done := make(chan protocol.Response, 1)
		go func(r protocol.Request) { done <- s.request(r) }(request)
		select {
		case r := <-done:
			if r.Error != "" {
				t.Fatal(r.Error)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("management operation blocked: %s", request.Op)
		}
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked by pane backpressure")
	}
}
