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

// maxPanes bounds a session's panes across all its windows.
const maxPanes = 32

type session struct {
	// mu owns layout, metadata and pane lookup. State order is session -> pane;
	// neither lock is held by the PTY writer or acquired under Server.mu.
	mu            sync.Mutex
	id            uint64 // immutable
	info          protocol.Session
	panes         map[int]*pane
	windows       []*window // in order; empty once the last pane has left
	active        int       // index into windows
	lastWindow    *window   // the window active before the current one, for last-window
	nextID        int
	width, height int
	shell         string
	revision      uint64
	notice        string // for the first client to attach, e.g. a layout file problem
	viewers       int    // attached clients whose terminal has focus
	edges         uint64 // directional moves that found no neighbor pane
	edgeDirection string
	stopped       bool
	done          chan struct{} // last pane has exited, stopped, and left layout
	watchers      sync.WaitGroup
	shutdownMu    sync.Mutex         // independent registry, never waits for state locks
	owned         map[*pane]struct{} // retains cleanup ownership before layout publication
	stopping      atomic.Bool
	stopOnce      sync.Once
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
		panes: map[int]*pane{1: p}, windows: []*window{{root: &layout{id: 1}, focused: 1}}, nextID: 2,
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
		i := s.windowOfLocked(id)
		if i < 0 {
			return
		}
		w := s.windows[i]
		root, neighbor := w.root.remove(id)
		w.root = root
		if w.last == id {
			w.last = 0
		}
		switch {
		case root == nil:
			s.removeWindowLocked(i)
		case w.focused == id:
			w.focused, w.zoomed = neighbor, false
		}
		if root != nil && root.id != 0 {
			w.zoomed = false // one pane left
		}
		if len(s.windows) == 0 {
			s.stopped = true
			close(s.done)
			return
		}
		s.updateViewedLocked()
		if !s.stopped && !s.stopping.Load() {
			_ = s.resizePanes()
		}
	}()
}

// Caller holds session.mu. The active window's panes as clients draw them.
func (s *session) placements() []placement {
	if w := s.activeLocked(); w != nil {
		return w.placements(s.width, s.height)
	}
	return nil
}

// resizePanes fits every window's panes to the session size, so switching
// windows shows them at once without resizing anything.
func (s *session) resizePanes() error {
	for _, win := range s.windows {
		for _, pos := range win.placements(s.width, s.height) {
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
	}
	return nil
}

func (s *session) status() protocol.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.info
	result.ID = s.id
	result.Panes = len(s.panes)
	result.Windows = len(s.windows)
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
	switch r.Op {
	case "navigate-pane":
		return s.navigatePane(r.Direction)
	case "close-pane":
		return s.closePane(r.PaneID)
	case "close-window":
		return s.closeWindow(r.Window)
	}
	// The new pane's directory is read from the OS before taking session.mu;
	// the split then targets the pane that was sampled.
	dir := ""
	if r.Op == "split" || r.Op == "new-window" {
		r.PaneID, dir = s.splitSource(r.PaneID)
	}
	s.mu.Lock()
	if s.stopped || s.stopping.Load() {
		s.mu.Unlock()
		return fmt.Errorf("session has been terminated")
	}
	id := r.PaneID
	if id == 0 {
		id = s.focusedLocked()
	}
	p := s.panes[id]
	if p == nil {
		s.mu.Unlock()
		return fmt.Errorf("pane not found")
	}
	// PTY input can be backpressured by the application. Don't hold the
	// workspace lock while writing it; other panes and shutdown remain usable.
	switch r.Op {
	case "key", "paste", "mouse":
		s.mu.Unlock()
		return p.input(r)
	}
	defer s.mu.Unlock()
	// Pane operations act in the window showing the pane, which is the
	// active one unless a pane ID names another.
	wi := s.windowOfLocked(id)
	if wi < 0 {
		return fmt.Errorf("pane not found")
	}
	w := s.windows[wi]
	switch r.Op {
	case "resize":
		s.width, s.height = dimensions(r.Width, r.Height)
	case "split":
		if r.Direction != "horizontal" && r.Direction != "vertical" {
			return fmt.Errorf("split direction must be horizontal or vertical")
		}
		if len(s.panes) >= maxPanes {
			return fmt.Errorf("a session can have at most %d panes", maxPanes)
		}
		// Check the unzoomed layout before launching a new shell.
		var positions []placement
		w.root.place(rect{0, 0, s.width, s.height}, &positions)
		var target rect
		for _, pos := range positions {
			if pos.id == id {
				target = pos.rect
			}
		}
		if target.w < 2 || target.h < 1 || (r.Direction == "horizontal" && target.w < 5) || (r.Direction == "vertical" && target.h < 3) {
			return fmt.Errorf("pane is too small to split; enlarge the terminal first")
		}
		width, height := target.w, target.h
		if r.Direction == "horizontal" {
			width = (width - 1) / 2
		} else {
			height = (height - 1) / 2
		}
		created, err := s.startPaneLocked(dir, width, height)
		if err != nil {
			return err
		}
		node := w.root.find(id)
		*node = layout{direction: r.Direction, ratio: 5000, first: &layout{id: id}, second: &layout{id: created}}
		w.focus(created)
		w.zoomed = false
		s.selectWindowLocked(wi)
	case "new-window":
		// Like tmux's new-window -c "#{pane_current_path}": the new window's
		// shell starts where the focused pane is.
		if err := validateWindowName(r.NewName); err != nil {
			return err
		}
		if len(s.panes) >= maxPanes {
			return fmt.Errorf("a session can have at most %d panes", maxPanes)
		}
		created, err := s.startPaneLocked(dir, s.width, s.height)
		if err != nil {
			return err
		}
		s.windows = append(s.windows, &window{name: r.NewName, root: &layout{id: created}, focused: created})
		s.selectWindowLocked(len(s.windows) - 1)
	case "select-window":
		i, err := s.windowTargetLocked(r.Window, r.Direction)
		if err != nil {
			return err
		}
		s.selectWindowLocked(i)
	case "rename-window":
		// An empty name goes back to showing the focused pane's title.
		i, err := s.windowNumberLocked(r.Window)
		if err != nil {
			return err
		}
		if err := validateWindowName(r.NewName); err != nil {
			return err
		}
		s.windows[i].name = r.NewName
	case "select-pane":
		if r.PaneID != 0 {
			// A pane in a background window, such as an agent opened from
			// the sidebar, brings its window forward.
			w.focus(id)
			s.selectWindowLocked(wi)
		} else if err := s.selectPaneDirectionLocked(w, r.Direction, w.focused); err != nil {
			return err
		}
	case "focus-pane":
		// Editor-side edge crossing (bmux focus left|right|up|down). The
		// request identifies the pane the editor lives in, not the client's
		// focused pane.
		if err := s.selectPaneDirectionLocked(w, r.Direction, id); err != nil {
			return err
		}
	case "resize-pane":
		if w.zoomed {
			return fmt.Errorf("unzoom before resizing panes")
		}
		if r.Direction != "left" && r.Direction != "right" && r.Direction != "up" && r.Direction != "down" {
			return fmt.Errorf("unknown resize direction")
		}
		w.root.resize(id, r.Direction, 5, rect{0, 0, s.width, s.height})
	case "drag-divider":
		// A mouse drag puts the divider between PaneID (left/top) and
		// NeighborID at column X of a side-by-side split, or row Y of a
		// stacked one. Focus doesn't move.
		if w.zoomed {
			return fmt.Errorf("unzoom before resizing panes")
		}
		at := r.X
		if r.Direction == "vertical" {
			at = r.Y
		}
		if !w.root.setDivider(id, r.NeighborID, r.Direction, at, rect{0, 0, s.width, s.height}) {
			return fmt.Errorf("those panes no longer share a divider")
		}
	case "swap-pane", "rotate-window", "select-layout", "last-pane", "break-pane", "join-pane":
		if err := s.arrangeLocked(r, wi, w, id); err != nil {
			return err
		}
	case "zoom":
		w.focus(id)
		w.zoomed = !w.zoomed
		s.selectWindowLocked(wi)
	default:
		return p.input(r)
	}
	return s.finishLayoutLocked()
}

// Caller holds session.mu. Starts a shell in dir ("" for the session's
// directory) as a new pane, registered for shutdown and watched, but not yet
// in any window's layout; the caller places it before releasing the lock.
func (s *session) startPaneLocked(dir string, width, height int) (int, error) {
	if dir == "" {
		dir = s.info.Dir
	}
	created, err := newPane(s.id, s.info.Name, dir, s.shell, width, height, s.nextID)
	if err != nil {
		return 0, err
	}
	s.shutdownMu.Lock()
	if s.stopping.Load() {
		s.shutdownMu.Unlock()
		created.stop()
		return 0, fmt.Errorf("session has been terminated")
	}
	s.owned[created] = struct{}{}
	s.shutdownMu.Unlock()
	id := s.nextID
	s.panes[id] = created
	s.watchPane(id, created)
	s.nextID++
	return id, nil
}

// Caller holds session.mu. Both explicit focus and editor-aware navigation use
// the same geometry and focus-notification rules.
func (s *session) finishLayoutLocked() error {
	s.revision++
	s.updateViewedLocked()
	return s.resizePanes()
}

// splitSource resolves the pane a split starts from (id, or the focused pane
// when 0) under session.mu, then reads its current directory with no state
// lock held. "" means the session's directory.
func (s *session) splitSource(id int) (int, string) {
	s.mu.Lock()
	if id == 0 {
		id = s.focusedLocked()
	}
	p := s.panes[id]
	s.mu.Unlock()
	if p == nil {
		return id, ""
	}
	return id, p.currentDir()
}

// Caller holds session.mu. Moves focus in window w to the nearest pane in
// direction from the pane identified by from; no neighbor is a silent no-op.
// Like tmux, a zoomed window searches the unzoomed layout and unzooms only
// when focus moves.
func (s *session) selectPaneDirectionLocked(w *window, direction string, from int) error {
	if _, err := editorNavigationKey(direction); err != nil {
		return err
	}
	if w == nil || w.root == nil {
		return nil
	}
	var positions []placement
	w.root.place(rect{0, 0, s.width, s.height}, &positions)
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
		w.focus(best)
		w.zoomed = false
	} else if len(positions) > 0 {
		// Clients use edges to move focus into their own chrome (the sidebar).
		s.edges++
		s.edgeDirection = direction
	}
	return nil
}

// snapshot coalesces revisions from the active window's PTYs while retaining a
// separate viewport for every pane in each attached client. Caller owns
// offsets and lastTotals.
func (s *session) snapshot(offsets, lastTotals map[int]int, last uint64, force bool) (*protocol.Frame, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revision := s.revisionLocked()
	if revision == last && !force {
		return nil, revision
	}
	f := &protocol.Frame{Name: s.info.Name, Focused: s.focusedLocked(), Width: s.width, Height: s.height, Generation: protocol.SessionGeneration(s.info.Created),
		Edge: s.edges, EdgeDirection: s.edgeDirection, Config: configRevision.Load(), Update: updateHint.Load(), Windows: s.windowsLocked()}
	if w := s.activeLocked(); w != nil {
		f.Zoomed = w.zoomed
	}
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
// Only the active window's panes count, so background output sends no frames;
// carryActiveLocked keeps the total increasing when that set changes.
func (s *session) revisionLocked() uint64 {
	revision := s.revision
	w := s.activeLocked()
	if w == nil {
		return revision
	}
	var ids []int
	w.root.leaves(&ids)
	for _, id := range ids {
		if p := s.panes[id]; p != nil {
			p.mu.Lock()
			revision += p.revision
			p.mu.Unlock()
		}
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
	f := &protocol.Frame{Name: s.info.Name, Generation: protocol.SessionGeneration(s.info.Created), PreviewMode: "terminal-viewport", Focused: s.focusedLocked(), Width: width, Height: height}
	w := s.activeLocked()
	if w == nil {
		return f, revision
	}
	f.Zoomed = w.zoomed
	positions := w.placements(width, height)
	for _, pos := range positions {
		f.Panes = append(f.Panes, protocol.PaneFrame{ID: pos.id, X: pos.x, Y: pos.y, Width: pos.w, Height: pos.h, Terminal: s.panes[pos.id].preview(pos.w, pos.h)})
	}
	return f, revision
}
