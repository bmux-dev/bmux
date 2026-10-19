package server

import (
	"fmt"
	"path/filepath"
)

// foregroundJob names the program in front of the pane's shell, or "" when
// the shell itself is in front. A recognized agent uses its agent name.
func (s *pane) foregroundJob() (string, error) {
	group, err := foregroundPTYGroup(s.pty)
	if err != nil {
		return "", err
	}
	if group <= 0 {
		return "", fmt.Errorf("PTY has no foreground process group")
	}
	if group == s.cmd.Process.Pid {
		return "", nil
	}
	s.mu.Lock()
	detected := s.detected
	s.mu.Unlock()
	if detected != "" {
		return detected, nil
	}
	name, err := foregroundGroupCommand(group, func(command string) string { return filepath.Base(command) })
	if name == "" && err == nil {
		name = "a program"
	}
	return name, err
}

func (s *session) livePane(id int) (*pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.stopping.Load() {
		return nil, fmt.Errorf("session has been terminated")
	}
	if id == 0 {
		id = s.focused
	}
	p := s.panes[id]
	if p == nil {
		return nil, fmt.Errorf("pane not found")
	}
	return p, nil
}

func (s *session) paneCommand(id int) (string, error) {
	p, err := s.livePane(id)
	if err != nil {
		return "", err
	}
	return p.foregroundJob()
}

// closePane ends a pane's shell and its jobs, like tmux's kill-pane. The pane
// watcher removes it from the layout once the shell is gone; closing the last
// pane ends the session. Stopping takes up to the shutdown budget, so it runs
// outside the caller's request handling.
func (s *session) closePane(id int) error {
	p, err := s.livePane(id)
	if err != nil {
		return err
	}
	go p.stop()
	return nil
}
