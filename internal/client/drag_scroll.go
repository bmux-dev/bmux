package client

import (
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Dragging a mouse selection above or below the focused pane scrolls its
// history toward the pointer, faster the further away it is, and keeps going
// while the pointer is held still, like tmux and most terminals. Each frame
// that arrives moves the selection's end to the edge row it scrolled to.

const dragScrollInterval = 50 * time.Millisecond

type dragScroll struct {
	active bool
	rows   int // rows per tick: positive scrolls up into history, negative down
	x      int // the pointer's column, clamped into the pane
	ticker *time.Ticker
}

// dragOutside handles a selection drag at pane-relative row y outside rows
// visible rows: start, speed up or slow down scrolling toward it.
func (u *ui) dragOutside(x, y, rows, width int) {
	speed := 0
	switch {
	case y < 0:
		speed = 1 + (-y-1)/2
	case y >= rows:
		speed = -(1 + (y-rows)/2)
	}
	if speed == 0 {
		u.stopDragScroll()
		return
	}
	u.drag.rows, u.drag.x = speed, max(0, min(x, width-1))
	if !u.drag.active {
		u.drag.active = true
		u.drag.ticker = time.NewTicker(dragScrollInterval)
		u.dragScrollTick() // move at once, not one interval later
	}
	u.extendToEdge(u.frame)
}

func (u *ui) stopDragScroll() {
	if u.drag.ticker != nil {
		u.drag.ticker.Stop()
	}
	u.drag = dragScroll{}
}

// dragScrollC is the ticker's channel, or nil (never ready) when stopped.
func (u *ui) dragScrollC() <-chan time.Time {
	if u.drag.ticker == nil {
		return nil
	}
	return u.drag.ticker.C
}

func (u *ui) dragScrollTick() {
	if !u.drag.active || !u.selection.dragging {
		u.stopDragScroll()
		return
	}
	u.scroll(u.drag.rows)
}

// extendToEdge moves the selection's end to the edge row being scrolled
// toward, in frame f.
func (u *ui) extendToEdge(f *protocol.TerminalFrame) {
	if !u.drag.active || !u.selection.dragging || f == nil || len(f.Rows) == 0 {
		return
	}
	edge := f.Base
	if u.drag.rows < 0 {
		edge = f.Base + len(f.Rows) - 1
	}
	u.selection.end = point{u.drag.x, edge}
}

// missingRows reports whether the selection spans rows this client never
// received, which a fast auto-scroll can skip.
func (s *selection) missingRows() bool {
	a, b := s.bounds()
	for y := a.y; y <= b.y; y++ {
		if _, ok := s.lines[y]; !ok {
			return true
		}
	}
	return false
}
