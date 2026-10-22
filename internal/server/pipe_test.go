package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestPipePaneCopiesOutputUntilStoppedOrTheCommandExits(t *testing.T) {
	srv, s := viewportSession(t)
	dir := t.TempDir()
	logA, logB := filepath.Join(dir, "a.log"), filepath.Join(dir, "b.log")
	run := func(text string) {
		t.Helper()
		if r := srv.request(protocol.Request{Op: "send-input", Name: "view", PaneID: 1, Input: []protocol.Input{{Paste: text}, {Code: uv.KeyEnter}}}); r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	piped := func() bool {
		t.Helper()
		panes, err := s.describe(paneTarget{pane: 1}, "pane")
		if err != nil {
			t.Fatal(err)
		}
		return panes[0].Pipe
	}
	eventually := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
		}
	}

	// #{...} in the command, and the pane's output in the file.
	if err := s.pipePane(paneTarget{pane: 1}, "cat >> "+dir+"/#{session_name}-#{pane_id}.log", false); err != nil {
		t.Fatal(err)
	}
	if !piped() {
		t.Fatal("pane_pipe is 0 while piping")
	}
	run("echo PIPED_$((1+1))")
	formatted := filepath.Join(dir, "view-1.log")
	eventually("output never reached the pipe", func() bool { data, _ := os.ReadFile(formatted); return strings.Contains(string(data), "PIPED_2") })

	// -o stops a piped pane; nothing more arrives.
	if err := s.pipePane(paneTarget{pane: 1}, "cat >> "+logA, true); err != nil || piped() {
		t.Fatalf("-o on a piped pane: %v, piped %v", err, piped())
	}
	run("echo AFTER_$((2+2))")
	awaitCapture(t, srv, 1, "AFTER_4")
	time.Sleep(100 * time.Millisecond)
	if data, _ := os.ReadFile(formatted); strings.Contains(string(data), "AFTER_4") {
		t.Fatal("output reached a stopped pipe")
	}
	// -o on an unpiped pane starts.
	if err := s.pipePane(paneTarget{pane: 1}, "cat >> "+logA, true); err != nil || !piped() {
		t.Fatalf("-o on an unpiped pane: %v, piped %v", err, piped())
	}
	run("echo TOGGLED_$((3+3))")
	eventually("-o didn't start piping", func() bool { data, _ := os.ReadFile(logA); return strings.Contains(string(data), "TOGGLED_6") })
	if err := s.pipePane(paneTarget{pane: 1}, "", false); err != nil || piped() {
		t.Fatalf("stop: %v, piped %v", err, piped())
	}

	// A command that exits by itself detaches.
	if err := s.pipePane(paneTarget{pane: 1}, "head -c 1 > "+logB, false); err != nil {
		t.Fatal(err)
	}
	run("echo X")
	eventually("a pipe whose command exited stayed attached", func() bool { return !piped() })

	// A command that never reads doesn't hold up the pane, and once stopped
	// it's ended after the grace period.
	saved := pipeGrace
	pipeGrace = 100 * time.Millisecond
	t.Cleanup(func() { pipeGrace = saved })
	if err := s.pipePane(paneTarget{pane: 1}, "exec sleep 30", false); err != nil {
		t.Fatal(err)
	}
	run("seq 1 100000 | tail -n 1; echo FLOOD_$((4+4))")
	awaitCapture(t, srv, 1, "FLOOD_8")
	s.mu.Lock()
	stuck := s.panes[1]
	s.mu.Unlock()
	stuck.mu.Lock()
	sleeper := stuck.pipe
	stuck.mu.Unlock()
	if err := s.pipePane(paneTarget{pane: 1}, "", false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sleeper.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("a stopped pipe's command that ignores end of file kept running")
	}

	// The pane's exit ends the pipe: the command reads end of file.
	exits := filepath.Join(dir, "exits")
	if err := s.pipePane(paneTarget{pane: 1}, "cat > /dev/null; echo eof > "+exits, false); err != nil {
		t.Fatal(err)
	}
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // keep the session
	if err := s.closePane(1); err != nil {
		t.Fatal(err)
	}
	eventually("the pipe's command never saw the pane end", func() bool { data, _ := os.ReadFile(exits); return strings.Contains(string(data), "eof") })

	if err := s.pipePane(paneTarget{pane: 7}, "cat", false); err == nil {
		t.Fatal("piping a missing pane was accepted")
	}
}
