package server

import (
	"fmt"
	"strings"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// describe answers bmux list-panes, list-windows and display-message: one
// PaneInfo per pane in scope, window by window in layout order. scope is
// "pane" (the target pane), "window" (the target's window) or "session".
// Directories and foreground programs are read from the OS after
// session.mu is released.
func (s *session) describe(t paneTarget, scope string) ([]protocol.PaneInfo, error) {
	type entry struct {
		info protocol.PaneInfo
		p    *pane
	}
	s.mu.Lock()
	if s.stopped || s.stopping.Load() {
		s.mu.Unlock()
		return nil, fmt.Errorf("session has been terminated")
	}
	only, onlyWindow := 0, 0
	switch scope {
	case "pane", "window":
		id, err := s.resolveLocked(t)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if scope == "pane" {
			only = id
		} else {
			onlyWindow, _ = s.placeLocked(id)
		}
	case "session":
	default:
		s.mu.Unlock()
		return nil, fmt.Errorf("unknown describe scope %q", scope)
	}
	var entries []entry
	for i, w := range s.windows {
		if onlyWindow != 0 && i+1 != onlyWindow {
			continue
		}
		var ids []int
		w.root.leaves(&ids)
		var unzoomed []placement
		w.root.place(rect{0, 0, s.width, s.height}, &unzoomed)
		// The name the status bar's tab shows: the window's name, else its
		// focused pane's title, else "shell" (client windowLabel).
		name := w.name
		if focused := s.panes[w.focused]; name == "" && focused != nil {
			focused.mu.Lock()
			name = strings.TrimSpace(focused.title)
			focused.mu.Unlock()
		}
		if name == "" {
			name = "shell"
		}
		for j, id := range ids {
			p := s.panes[id]
			if p == nil || (only != 0 && id != only) {
				continue
			}
			info := protocol.PaneInfo{
				Session: s.info.Name, SessionID: s.id, SessionPath: s.info.Dir, SessionWindows: len(s.windows), SessionAttached: s.info.Clients,
				Window: i + 1, WindowName: name, WindowActive: i == s.active, WindowZoomed: w.zoomed, WindowPanes: len(ids),
				Pane: id, Index: j + 1, Active: id == w.focused,
			}
			if w.zoomed && id == w.focused {
				info.Width, info.Height = s.width, s.height
			} else {
				for _, pos := range unzoomed {
					if pos.id == id {
						info.Width, info.Height = pos.rect.w, pos.rect.h
					}
				}
			}
			p.mu.Lock()
			info.Title = p.title
			a, _ := p.activityLocked(id)
			info.Agent, info.State = a.Agent, a.State
			info.Pipe = p.pipe != nil
			info.MonitorActivity = p.monitorActivity
			info.MonitorSilence = int(p.monitorSilence / time.Second)
			p.mu.Unlock()
			entries = append(entries, entry{info, p})
		}
	}
	s.mu.Unlock()
	result := make([]protocol.PaneInfo, len(entries))
	for i, e := range entries {
		e.info.Path = e.p.currentDir()
		e.info.Command, _ = e.p.foregroundJob()
		result[i] = e.info
	}
	return result, nil
}
