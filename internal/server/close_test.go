package server

import (
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestClosePaneNamesTheJobInFrontAndEndsIt(t *testing.T) {
	s := editorNavigationSession(t) // panes 1 and 2
	waitEditorForeground(t, s.panes[1], false)
	if command, err := s.paneCommand(1); err != nil || command != "" {
		t.Fatalf("shell in front: %q %v", command, err)
	}
	editor := startTestEditor(t, s, 1) // a program named nvim in front of pane 1's shell
	if command, err := s.paneCommand(1); err != nil || command != "nvim" {
		t.Fatalf("program in front: %q %v", command, err)
	}

	if err := s.input(protocol.Request{Op: "close-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-editor.stopDone: // its jobs, including the program, were stopped
	case <-time.After(5 * time.Second):
		t.Fatal("closing the pane did not stop its jobs")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		gone, focused := s.panes[1] == nil, s.focused
		s.mu.Unlock()
		if gone && focused == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed pane stayed in the layout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.input(protocol.Request{Op: "close-pane", PaneID: 1}); err == nil {
		t.Fatal("closing a missing pane succeeded")
	}

	// Closing the last pane ends the session, like exiting its shell.
	if err := s.input(protocol.Request{Op: "close-pane", PaneID: 2}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the last pane did not end the session")
	}
}
