package server

import (
	"fmt"
	"path/filepath"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

func isEditorCommand(command string) bool {
	switch filepath.Base(command) {
	case "nvim", "neovim", "nvimdiff", "vim", "vimdiff", "view", "gvim", "gvimdiff":
		return true
	}
	return false
}

func editorMatch(command string) string {
	if isEditorCommand(command) {
		return "editor"
	}
	return ""
}

func editorNavigationKey(direction string) (protocol.Request, error) {
	var key rune
	switch direction {
	case "left":
		key = 'h'
	case "down":
		key = 'j'
	case "up":
		key = 'k'
	case "right":
		key = 'l'
	default:
		return protocol.Request{}, fmt.Errorf("unknown pane direction")
	}
	return protocol.Request{Op: "key", Code: key, Mod: int(uv.ModCtrl)}, nil
}

// Query the OS foreground group only on a navigation key, never per frame and
// never while holding session/emulator locks. Titles are not process identity.
func (p *pane) foregroundEditor() (bool, error) {
	group, err := foregroundPTYGroup(p.pty)
	if err != nil {
		return false, err
	}
	if group <= 0 {
		return false, fmt.Errorf("PTY has no foreground process group")
	}
	name, err := foregroundGroupCommand(group, editorMatch)
	return name != "", err
}

func (s *session) navigatePane(direction string) error {
	key, err := editorNavigationKey(direction)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		s.mu.Lock()
		if s.stopped || s.stopping.Load() {
			s.mu.Unlock()
			return fmt.Errorf("session has been terminated")
		}
		id := s.focused
		p := s.panes[id]
		s.mu.Unlock()
		if p == nil {
			return fmt.Errorf("pane not found")
		}
		editor, queryErr := p.foregroundEditor()
		s.mu.Lock()
		if s.stopped || s.stopping.Load() {
			s.mu.Unlock()
			return fmt.Errorf("session has been terminated")
		}
		if s.focused != id || s.panes[id] != p {
			s.mu.Unlock()
			continue
		}
		if editor || queryErr != nil {
			// If process inspection fails, preserve application input rather than
			// silently moving away from an editor we could not identify.
			s.mu.Unlock()
			return p.input(key)
		}
		err := s.selectPaneDirectionLocked(direction, s.focused)
		if err == nil {
			err = s.finishLayoutLocked()
		}
		s.mu.Unlock()
		return err
	}
	return fmt.Errorf("pane focus changed; navigation cancelled")
}
