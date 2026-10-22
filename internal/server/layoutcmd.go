package server

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Layout commands (bmux split, new-window, select, kill-pane, kill-window,
// resize, zoom, rename-window) are the session ops an attached client sends
// from its keys, reached from a script with a target instead. Like bmux
// send, they never resize the session.

// layoutCommand runs r.Kind on the pane or window r targets and returns the
// pane it leaves in question: the new one after split and new-window, else
// the target.
func (s *session) layoutCommand(r protocol.Request) (protocol.PaneActivity, error) {
	t := targetOf(r)
	s.mu.Lock()
	if s.stopped || s.stopping.Load() {
		s.mu.Unlock()
		return protocol.PaneActivity{}, fmt.Errorf("session has been terminated")
	}
	id, err := s.resolveLocked(t)
	window, _ := s.placeLocked(id)
	previousActive, previousFocus := s.active, s.focusedLocked()
	s.mu.Unlock()
	if err != nil {
		return protocol.PaneActivity{}, err
	}
	// A window target (SESSION:WINDOW.) names the window itself.
	windowOnly := t.window != 0 && !t.named()
	if r.Dir != "" {
		if !filepath.IsAbs(r.Dir) {
			return protocol.PaneActivity{}, fmt.Errorf("directory %q must be absolute", r.Dir)
		}
		if info, err := os.Stat(r.Dir); err != nil || !info.IsDir() {
			return protocol.PaneActivity{}, fmt.Errorf("%s is not a directory", r.Dir)
		}
	}
	switch r.Kind {
	case "split":
		direction := r.Direction
		if direction == "" {
			direction = "vertical"
		}
		if err := s.input(protocol.Request{Op: "split", PaneID: id, Direction: direction, Dir: r.Dir}); err != nil {
			return protocol.PaneActivity{}, err
		}
		return s.settleNew(r, previousActive, previousFocus)
	case "new-window":
		if err := s.input(protocol.Request{Op: "new-window", PaneID: id, NewName: r.NewName, Dir: r.Dir}); err != nil {
			return protocol.PaneActivity{}, err
		}
		return s.settleNew(r, previousActive, previousFocus)
	case "select":
		if windowOnly {
			err = s.input(protocol.Request{Op: "select-window", Window: window})
		} else {
			err = s.input(protocol.Request{Op: "select-pane", PaneID: id})
		}
	case "kill-pane":
		if windowOnly {
			return protocol.PaneActivity{}, fmt.Errorf("kill-pane needs a pane; use kill-window for a window")
		}
		err = s.closePane(id)
	case "kill-window":
		err = s.closeWindow(window)
	case "resize":
		if windowOnly {
			return protocol.PaneActivity{}, fmt.Errorf("resize needs a pane")
		}
		err = s.input(protocol.Request{Op: "resize-pane", PaneID: id, Direction: r.Direction, Delta: r.Delta})
	case "zoom":
		err = s.input(protocol.Request{Op: "zoom", PaneID: id})
	case "rename-window":
		err = s.input(protocol.Request{Op: "rename-window", Window: window, NewName: r.NewName})
	default:
		return protocol.PaneActivity{}, fmt.Errorf("unknown layout command %q", r.Kind)
	}
	if err != nil {
		return protocol.PaneActivity{}, err
	}
	return protocol.PaneActivity{Pane: id, Window: window}, nil
}

// settleNew finishes split and new-window: the new pane is the focused pane
// of the window the op left active. With Detach (-d), focus and the active
// window go back to where they were, like tmux's -d. A command is typed into
// the new shell like a layout file's.
func (s *session) settleNew(r protocol.Request, previousActive, previousFocus int) (protocol.PaneActivity, error) {
	s.mu.Lock()
	w := s.activeLocked()
	if w == nil {
		s.mu.Unlock()
		return protocol.PaneActivity{}, fmt.Errorf("session has no windows")
	}
	created := w.focused
	if r.Detach {
		if previous := s.windowOfLocked(previousFocus); previous >= 0 {
			s.windows[previous].focus(previousFocus)
		}
		if previousActive < len(s.windows) && s.active != previousActive {
			s.selectWindowLocked(previousActive)
		}
		_ = s.finishLayoutLocked()
	}
	window, index := s.placeLocked(created)
	p := s.panes[created]
	s.mu.Unlock()
	if command := layoutCommandLine(r.Args); command != "" && p != nil {
		if err := p.input(protocol.Request{Op: "type", Text: command + "\r"}); err != nil {
			return protocol.PaneActivity{}, err
		}
	}
	return protocol.PaneActivity{Pane: created, Window: window, Index: index}, nil
}

// layoutCommandLine is the text typed for a split's or new window's command:
// one argument is a shell command line as written, several are an argv.
func layoutCommandLine(args []string) string {
	switch len(args) {
	case 0:
		return ""
	case 1:
		return args[0]
	}
	return shellCommand(args)
}
