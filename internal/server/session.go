package server

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Session IDs never change or get reused while the server runs, unlike names.
var sessionIDs atomic.Uint64

type session struct {
	// mu owns layout, metadata and pane lookup. State order is session -> pane;
	// neither lock is held by the PTY writer or acquired under Server.mu.
	mu              sync.Mutex
	id              uint64 // immutable
	info            protocol.Session
	panes           map[int]*pane
	root            *layout
	focused, nextID int
	width, height   int
	zoomed          bool
	shell           string
	revision        uint64
	viewers         int    // attached clients whose terminal has focus
	edges           uint64 // directional moves that found no neighbor pane
	edgeDirection   string
	stopped         bool
	done            chan struct{} // last pane has exited, stopped, and left layout
	watchers        sync.WaitGroup
	shutdownMu      sync.Mutex         // independent registry, never waits for state locks
	owned           map[*pane]struct{} // retains cleanup ownership before layout publication
	stopping        atomic.Bool
	stopOnce        sync.Once
}

func newSession(name, dir, shell string, width, height int) (*session, error) {
	w, h := dimensions(width, height)
	id := sessionIDs.Add(1)
	p, err := newPane(id, name, dir, shell, w, h, 1)
	if err != nil {
		return nil, err
	}
	s := &session{
		id:    id,
		info:  protocol.Session{Name: name, Dir: dir, Created: time.Now(), Used: time.Now(), Panes: 1},
		panes: map[int]*pane{1: p}, root: &layout{id: 1}, focused: 1, nextID: 2,
		width: w, height: h, shell: shell, revision: 1, done: make(chan struct{}), owned: map[*pane]struct{}{p: {}},
	}
	s.watchPane(1, p)
	go s.watchAgents()
	return s, nil
}

func (s *session) watchPane(id int, p *pane) {
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		<-p.done
		// A background child might still hold the PTY open after its shell exits.
		// Shell lifetime, not PTY EOF or client attachment, determines pane lifetime.
		p.stop()
		s.shutdownMu.Lock()
		delete(s.owned, p)
		s.shutdownMu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.panes[id] != p {
			return
		}
		p.mu.Lock()
		// Carry the removed pane's contribution into the session revision so
		// snapshot revisions remain monotonic even when a busy pane disappears.
		s.revision += p.revision + 1
		p.mu.Unlock()
		delete(s.panes, id)
		root, neighbor := s.root.remove(id)
		s.root = root
		if root == nil {
			s.focused = 0
			s.zoomed = false
			s.stopped = true
			close(s.done)
			return
		}
		if s.focused == id {
			s.focused = neighbor
			s.zoomed = false
			if !s.stopped && !s.stopping.Load() {
				_ = s.panes[s.focused].input(protocol.Request{Op: "focus", Focused: true})
			}
			s.updateViewedLocked()
		}
		if len(s.panes) == 1 {
			s.zoomed = false
		}
		if !s.stopped && !s.stopping.Load() {
			_ = s.resizePanes()
		}
	}()
}

func (s *session) placements() []placement {
	if s.root == nil {
		return nil
	}
	if s.zoomed {
		return []placement{{s.focused, rect{0, 0, s.width, s.height}}}
	}
	var result []placement
	s.root.place(rect{0, 0, s.width, s.height}, &result)
	return result
}

func (s *session) resizePanes() error {
	for _, pos := range s.placements() {
		p := s.panes[pos.id]
		p.mu.Lock()
		w, h := max(2, pos.w), max(1, pos.h)
		changed := p.term.Width() != w || p.term.Height() != h
		p.mu.Unlock()
		if changed {
			if err := p.input(protocol.Request{Op: "resize", Width: w, Height: h}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *session) status() protocol.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.info
	result.Panes = len(s.panes)
	result.Activity = s.activityLocked()
	result.Exited = true
	for _, p := range s.panes {
		p.mu.Lock()
		result.Exited = result.Exited && p.info.Exited
		p.mu.Unlock()
	}
	return result
}

func (s *session) stop() {
	s.stopUntil(time.Now().Add(paneShutdownBudget))
}

// stopUntil lets the server give every session the same shutdown deadline.
func (s *session) stopUntil(deadline time.Time) {
	s.stopOnce.Do(func() {
		s.stopping.Store(true)
		// This registry never waits behind session layout/emulator operations.
		s.shutdownMu.Lock()
		items := make([]*pane, 0, len(s.owned))
		for p := range s.owned {
			items = append(items, p)
		}
		s.shutdownMu.Unlock()
		var wg sync.WaitGroup
		for _, p := range items {
			wg.Add(1)
			go func() { defer wg.Done(); p.stopUntil(deadline) }()
		}
		wg.Wait()
		select {
		case <-s.done:
		case <-time.After(max(time.Duration(0), time.Until(deadline))):
		}
	})
}

func (s *session) input(r protocol.Request) error {
	if r.Op == "navigate-pane" {
		return s.navigatePane(r.Direction)
	}
	if r.Op == "close-pane" {
		return s.closePane(r.PaneID)
	}
	s.mu.Lock()
	if s.stopped || s.stopping.Load() {
		s.mu.Unlock()
		return fmt.Errorf("session has been terminated")
	}
	id := r.PaneID
	if id == 0 {
		id = s.focused
	}
	p := s.panes[id]
	if p == nil {
		s.mu.Unlock()
		return fmt.Errorf("pane not found")
	}
	// PTY input can be backpressured by the application. Don't hold the
	// workspace lock while writing it; other panes and shutdown remain usable.
	switch r.Op {
	case "key", "paste", "focus", "mouse":
		s.mu.Unlock()
		return p.input(r)
	}
	defer s.mu.Unlock()
	previous := s.focused
	switch r.Op {
	case "resize":
		s.width, s.height = dimensions(r.Width, r.Height)
	case "split":
		if r.Direction != "horizontal" && r.Direction != "vertical" {
			return fmt.Errorf("split direction must be horizontal or vertical")
		}
		if len(s.panes) >= 32 {
			return fmt.Errorf("a session can have at most 32 panes")
		}
		// Check the unzoomed layout before launching a new shell.
		var positions []placement
		s.root.place(rect{0, 0, s.width, s.height}, &positions)
		var target rect
		for _, pos := range positions {
			if pos.id == id {
				target = pos.rect
			}
		}
		if target.w < 2 || target.h < 1 || (r.Direction == "horizontal" && target.w < 5) || (r.Direction == "vertical" && target.h < 3) {
			return fmt.Errorf("pane is too small to split; enlarge the terminal first")
		}
		w, h := target.w, target.h
		if r.Direction == "horizontal" {
			w = (w - 1) / 2
		} else {
			h = (h - 1) / 2
		}
		created, err := newPane(s.id, s.info.Name, s.info.Dir, s.shell, w, h, s.nextID)
		if err != nil {
			return err
		}
		s.shutdownMu.Lock()
		if s.stopping.Load() {
			s.shutdownMu.Unlock()
			created.stop()
			return fmt.Errorf("session has been terminated")
		}
		s.owned[created] = struct{}{}
		s.shutdownMu.Unlock()
		node := s.root.find(id)
		*node = layout{direction: r.Direction, ratio: 5000, first: &layout{id: id}, second: &layout{id: s.nextID}}
		s.panes[s.nextID] = created
		s.watchPane(s.nextID, created)
		s.focused = s.nextID
		s.nextID++
		s.zoomed = false
	case "select-pane":
		if r.PaneID != 0 {
			s.focused = id
		} else {
			if err := s.selectPaneDirectionLocked(r.Direction, s.focused); err != nil {
				return err
			}
		}
	case "focus-pane":
		// Editor-side edge crossing (bmux focus left|right|up|down). The
		// request identifies the pane the editor lives in, not the client's
		// focused pane.
		if err := s.selectPaneDirectionLocked(r.Direction, id); err != nil {
			return err
		}
	case "resize-pane":
		if s.zoomed {
			return fmt.Errorf("unzoom before resizing panes")
		}
		if r.Direction != "left" && r.Direction != "right" && r.Direction != "up" && r.Direction != "down" {
			return fmt.Errorf("unknown resize direction")
		}
		s.root.resize(id, r.Direction, 5, rect{0, 0, s.width, s.height})
	case "zoom":
		s.focused = id
		s.zoomed = !s.zoomed
	default:
		return p.input(r)
	}
	return s.finishLayoutLocked(previous)
}

// Caller holds session.mu. Both explicit focus and editor-aware navigation use
// the same geometry and focus-notification rules.
func (s *session) finishLayoutLocked(previous int) error {
	s.revision++
	if previous != s.focused {
		_ = s.panes[previous].input(protocol.Request{Op: "focus", Focused: false})
		_ = s.panes[s.focused].input(protocol.Request{Op: "focus", Focused: true})
	}
	s.updateViewedLocked()
	return s.resizePanes()
}

// Caller holds session.mu. Moves focus to the nearest pane in direction from
// the pane identified by from; no neighbor is a silent no-op. Like tmux, a
// zoomed session searches the unzoomed layout and unzooms only when focus moves.
func (s *session) selectPaneDirectionLocked(direction string, from int) error {
	if _, err := editorNavigationKey(direction); err != nil {
		return err
	}
	if s.root == nil {
		return nil
	}
	var positions []placement
	s.root.place(rect{0, 0, s.width, s.height}, &positions)
	var current rect
	for _, pos := range positions {
		if pos.id == from {
			current = pos.rect
		}
	}
	best, bestDistance := 0, int(^uint(0)>>1)
	for _, pos := range positions {
		if pos.id == from || pos.w == 0 || pos.h == 0 {
			continue
		}
		distance := 0
		switch direction {
		case "left":
			if pos.x+pos.w > current.x || pos.y >= current.y+current.h || pos.y+pos.h <= current.y {
				continue
			}
			distance = current.x - pos.x - pos.w
		case "right":
			if pos.x < current.x+current.w || pos.y >= current.y+current.h || pos.y+pos.h <= current.y {
				continue
			}
			distance = pos.x - current.x - current.w
		case "up":
			if pos.y+pos.h > current.y || pos.x >= current.x+current.w || pos.x+pos.w <= current.x {
				continue
			}
			distance = current.y - pos.y - pos.h
		case "down":
			if pos.y < current.y+current.h || pos.x >= current.x+current.w || pos.x+pos.w <= current.x {
				continue
			}
			distance = pos.y - current.y - current.h
		}
		if distance < bestDistance {
			best, bestDistance = pos.id, distance
		}
	}
	if best != 0 {
		s.focused = best
		s.zoomed = false
	} else if len(positions) > 0 {
		// Clients use edges to move focus into their own chrome (the sidebar).
		s.edges++
		s.edgeDirection = direction
	}
	return nil
}

// snapshot coalesces revisions from all PTYs while retaining a separate viewport
// for every pane in each attached client. Caller owns offsets and lastTotals.
func (s *session) snapshot(offsets, lastTotals map[int]int, last uint64, force bool) (*protocol.Frame, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision := s.revisionLocked()
	if revision == last && !force {
		return nil, revision
	}
	f := &protocol.Frame{Name: s.info.Name, Focused: s.focused, Zoomed: s.zoomed, Width: s.width, Height: s.height, Generation: protocol.SessionGeneration(s.info.Created),
		Edge: s.edges, EdgeDirection: s.edgeDirection, Config: configRevision.Load()}
	for id := range offsets {
		if s.panes[id] == nil {
			delete(offsets, id)
			delete(lastTotals, id)
		}
	}
	for _, pos := range s.placements() {
		p := s.panes[pos.id]
		terminal, _ := p.anchoredFrame(offsets[pos.id], lastTotals[pos.id])
		offsets[pos.id], lastTotals[pos.id] = terminal.Offset, terminal.Base+terminal.Offset
		f.Panes = append(f.Panes, protocol.PaneFrame{ID: pos.id, X: pos.x, Y: pos.y, Width: pos.w, Height: pos.h, Terminal: terminal})
	}
	return f, revision
}

// preview uses a panel-sized layout and bounded native-cell viewports, never PTY resizing,
// attachment, input, focus changes, or updates to the session's recent-use time.
func (s *session) preview(width, height int) *protocol.Frame {
	f, _ := s.previewSnapshot(width, height, 0, true)
	return f
}

// Caller holds session.mu; pane revisions are sampled in the normal lock order.
func (s *session) revisionLocked() uint64 {
	revision := s.revision
	for _, p := range s.panes {
		p.mu.Lock()
		revision += p.revision
		p.mu.Unlock()
	}
	return revision
}

func (s *session) previewSnapshot(width, height int, last uint64, force bool) (*protocol.Frame, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision := s.revisionLocked()
	if revision == last && !force {
		return nil, revision
	}
	// Bound untrusted geometry, not the UI's normal window sizes. Cell copies
	// remain limited by each pane's actual terminal dimensions in pane.preview.
	width = max(2, min(width, protocol.MaxPreviewWidth))
	height = max(1, min(height, protocol.MaxPreviewHeight))
	f := &protocol.Frame{Name: s.info.Name, Generation: protocol.SessionGeneration(s.info.Created), PreviewMode: "terminal-viewport", Focused: s.focused, Zoomed: s.zoomed, Width: width, Height: height}
	if s.root == nil {
		return f, revision
	}
	var positions []placement
	if s.zoomed {
		positions = []placement{{s.focused, rect{0, 0, width, height}}}
	} else {
		s.root.place(rect{0, 0, width, height}, &positions)
	}
	for _, pos := range positions {
		f.Panes = append(f.Panes, protocol.PaneFrame{ID: pos.id, X: pos.x, Y: pos.y, Width: pos.w, Height: pos.h, Terminal: s.panes[pos.id].preview(pos.w, pos.h)})
	}
	return f, revision
}
