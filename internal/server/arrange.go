package server

import (
	"fmt"
	"slices"

	"github.com/josephferrero/bmux/internal/protocol"
)

// arrangeLocked runs the operations that move panes around: within window w
// (swap-pane, rotate-window, select-layout, last-pane) or between windows
// (break-pane, join-pane). id is the pane acted on, in window index wi.
// Caller holds session.mu and finishes the layout afterwards.
func (s *session) arrangeLocked(r protocol.Request, wi int, w *window, id int) error {
	switch r.Op {
	case "swap-pane":
		// Like tmux's swap-pane -U / -D: trade places with the previous or
		// next pane in layout order. Focus follows the pane.
		if w.zoomed {
			return fmt.Errorf("unzoom before moving panes")
		}
		var ids []int
		w.root.leaves(&ids)
		i := slices.Index(ids, id)
		j := 0
		switch r.Direction {
		case "previous":
			j = (i + len(ids) - 1) % len(ids)
		case "next":
			j = (i + 1) % len(ids)
		default:
			return fmt.Errorf("swap direction must be previous or next")
		}
		ids[i], ids[j] = ids[j], ids[i]
		w.root.setLeaves(ids)
	case "rotate-window":
		// Like tmux's rotate-window: every pane moves one place forward in
		// layout order and the last takes the first place.
		if w.zoomed {
			return fmt.Errorf("unzoom before moving panes")
		}
		var ids []int
		w.root.leaves(&ids)
		w.root.setLeaves(append(ids[len(ids)-1:], ids[:len(ids)-1]...))
	case "select-layout":
		var ids []int
		w.root.leaves(&ids)
		if r.Kind == "equalize" {
			w.root.equalize()
			break
		}
		name := r.Kind
		if name == "next" {
			name = layoutPresets[w.preset%len(layoutPresets)]
		}
		root, err := presetLayout(name, ids)
		if err != nil {
			return err
		}
		w.root, w.zoomed = root, false
		w.preset = slices.Index(layoutPresets, name) + 1
	case "last-pane":
		// Like tmux's last-pane; nothing to go back to is a quiet no-op.
		if w.last != 0 && w.root.contains(w.last) {
			w.focus(w.last)
		}
	case "break-pane":
		return s.breakPaneLocked(w, id)
	case "join-pane":
		return s.joinPaneLocked(wi, w, id)
	}
	return nil
}

// Caller holds session.mu. detachLocked takes pane id out of window w,
// keeping its process; it reports whether w is left empty.
func (s *session) detachLocked(w *window, id int) bool {
	root, neighbor := w.root.remove(id)
	w.root = root
	if w.last == id {
		w.last = 0
	}
	if root == nil {
		return true
	}
	if w.focused == id {
		w.focused = neighbor
	}
	w.zoomed = false
	return false
}

// breakPaneLocked moves pane id out of window w into a new window of its own,
// like tmux's break-pane, and makes that window active.
func (s *session) breakPaneLocked(w *window, id int) error {
	if w.root.id != 0 {
		return fmt.Errorf("the window has only one pane")
	}
	s.carryActiveLocked()
	s.detachLocked(w, id)
	s.windows = append(s.windows, &window{root: &layout{id: id}, focused: id})
	s.selectWindowLocked(len(s.windows) - 1)
	return nil
}

// joinPaneLocked moves pane id from window index wi into the last window,
// side by side with that window's focused pane, like tmux's join-pane -h -t !.
// An emptied window closes; the joined window becomes active.
func (s *session) joinPaneLocked(wi int, w *window, id int) error {
	target := s.lastWindow
	if s.indexLocked(target) < 0 || target == w {
		return fmt.Errorf("no last window to join")
	}
	var positions []placement
	target.root.place(rect{0, 0, s.width, s.height}, &positions)
	for _, pos := range positions {
		if pos.id == target.focused && pos.w < 5 {
			return fmt.Errorf("the last window's pane is too small to join; enlarge the terminal first")
		}
	}
	s.carryActiveLocked()
	if s.detachLocked(w, id) {
		s.windows = slices.Delete(s.windows, wi, wi+1)
		w = nil
	}
	node := target.root.find(target.focused)
	*node = layout{direction: "horizontal", ratio: 5000, first: &layout{id: target.focused}, second: &layout{id: id}}
	target.focus(id)
	target.zoomed = false
	// Back to the window the pane came from, while it's there.
	s.active, s.lastWindow = s.indexLocked(target), w
	return nil
}
