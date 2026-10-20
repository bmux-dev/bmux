package server

import (
	"fmt"
	"unicode/utf8"

	"github.com/josephferrero/bmux/internal/protocol"
)

// A window is one split layout in a session, like a tmux window. Panes live in
// the session's map, so pane IDs stay unique across a session's windows; a
// window's tree names the panes it shows. Every window is laid out at the
// session's size, so switching never resizes a pane.
type window struct {
	name    string // "" shows the focused pane's title
	root    *layout
	focused int
	last    int // previously focused pane, for last-pane; 0 if none
	zoomed  bool
}

const maxWindowName = 40

// focus moves focus to id, remembering the pane it leaves for last-pane.
func (w *window) focus(id int) {
	if id != w.focused {
		w.last, w.focused = w.focused, id
	}
}

func (w *window) placements(width, height int) []placement {
	if w.root == nil {
		return nil
	}
	if w.zoomed {
		return []placement{{w.focused, rect{0, 0, width, height}}}
	}
	var result []placement
	w.root.place(rect{0, 0, width, height}, &result)
	return result
}

// Caller holds session.mu. nil only once the last window has closed.
func (s *session) activeLocked() *window {
	if len(s.windows) == 0 {
		return nil
	}
	return s.windows[s.active]
}

// Caller holds session.mu. The focused pane of the active window, or 0.
func (s *session) focusedLocked() int {
	if w := s.activeLocked(); w != nil {
		return w.focused
	}
	return 0
}

// Caller holds session.mu. The index of the window showing pane id, or -1.
func (s *session) windowOfLocked(id int) int {
	for i, w := range s.windows {
		if w.root != nil && w.root.contains(id) {
			return i
		}
	}
	return -1
}

// carryActiveLocked runs before the set of panes in the active window
// changes. Snapshots sum the revisions of the active window's panes only, so
// output in background windows sends no frames; carrying the outgoing sum into
// the session revision keeps the total increasing across the change, so a
// client can never mistake the new window for a frame it already has.
func (s *session) carryActiveLocked() {
	w := s.activeLocked()
	if w == nil || w.root == nil {
		s.revision++
		return
	}
	var ids []int
	w.root.leaves(&ids)
	for _, id := range ids {
		if p := s.panes[id]; p != nil {
			p.mu.Lock()
			s.revision += p.revision
			p.mu.Unlock()
		}
	}
	s.revision++
}

// Caller holds session.mu. Switches the active window, remembering the one it
// leaves for last-window.
func (s *session) selectWindowLocked(i int) {
	if i == s.active || i < 0 || i >= len(s.windows) {
		return
	}
	s.carryActiveLocked()
	s.lastWindow = s.windows[s.active]
	s.active = i
}

// Caller holds session.mu. Removes an empty window. Leaving the active one
// returns to the last window, like tmux, or else the one now in its place.
func (s *session) removeWindowLocked(i int) {
	removed := s.windows[i]
	if i == s.active {
		s.carryActiveLocked()
	}
	s.windows = append(s.windows[:i], s.windows[i+1:]...)
	if s.lastWindow == removed {
		s.lastWindow = nil
	}
	switch {
	case len(s.windows) == 0:
		s.active = 0
	case i == s.active:
		s.active = min(i, len(s.windows)-1)
		if j := s.indexLocked(s.lastWindow); j >= 0 {
			s.active = j
			s.lastWindow = nil
		}
	case i < s.active:
		s.active--
	}
}

// Caller holds session.mu. The index of w, or -1.
func (s *session) indexLocked(w *window) int {
	for i, candidate := range s.windows {
		if candidate == w && w != nil {
			return i
		}
	}
	return -1
}

// Caller holds session.mu. Resolves select-window's target: a 1-based number,
// or next, previous or last.
func (s *session) windowTargetLocked(number int, direction string) (int, error) {
	switch direction {
	case "":
		if number < 1 || number > len(s.windows) {
			return 0, fmt.Errorf("no window %d", number)
		}
		return number - 1, nil
	case "next":
		return (s.active + 1) % len(s.windows), nil
	case "previous":
		return (s.active + len(s.windows) - 1) % len(s.windows), nil
	case "last":
		if i := s.indexLocked(s.lastWindow); i >= 0 {
			return i, nil
		}
		return 0, fmt.Errorf("no last window")
	}
	return 0, fmt.Errorf("unknown window direction %q", direction)
}

// Caller holds session.mu. number 0 means the active window.
func (s *session) windowNumberLocked(number int) (int, error) {
	if number == 0 {
		return s.active, nil
	}
	return s.windowTargetLocked(number, "")
}

func validateWindowName(name string) error {
	if len(name) > maxWindowName || !utf8.ValidString(name) {
		return fmt.Errorf("window names are at most %d bytes", maxWindowName)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("window names can't contain control characters")
		}
	}
	return nil
}

// Caller holds session.mu. Window descriptions for snapshots, in order.
func (s *session) windowsLocked() []protocol.Window {
	result := make([]protocol.Window, len(s.windows))
	for i, w := range s.windows {
		info := protocol.Window{Name: w.name, Active: i == s.active}
		if w.root != nil {
			w.root.leaves(&info.Panes)
		}
		if p := s.panes[w.focused]; p != nil && w.name == "" {
			p.mu.Lock()
			info.Title = p.title
			p.mu.Unlock()
		}
		result[i] = info
	}
	return result
}
