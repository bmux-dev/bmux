package server

// A binary layout tree keeps nested splits and their proportions independent
// from PTY lifetime. Every divider consumes exactly one screen cell.
type layout struct {
	id            int
	direction     string
	ratio         int // ten-thousandths, first child's share
	first, second *layout
}

type rect struct{ x, y, w, h int }
type placement struct {
	id int
	rect
}

func (n *layout) minimum() (int, int) {
	if n.id != 0 {
		return 2, 1
	}
	aw, ah := n.first.minimum()
	bw, bh := n.second.minimum()
	if n.direction == "horizontal" {
		return aw + bw + 1, max(ah, bh)
	}
	return max(aw, bw), ah + bh + 1
}

func (n *layout) place(r rect, out *[]placement) {
	if n.id != 0 {
		*out = append(*out, placement{n.id, r})
		return
	}
	aw, ah := n.first.minimum()
	bw, bh := n.second.minimum()
	a, b := r, r
	if n.direction == "horizontal" {
		available := max(0, r.w-1)
		size := available * n.ratio / 10000
		if available >= aw+bw {
			size = max(aw, min(size, available-bw))
		} else {
			size = max(0, min(size, available))
		}
		a.w = size
		b.x = r.x + size + 1
		b.w = available - size
	} else {
		available := max(0, r.h-1)
		size := available * n.ratio / 10000
		if available >= ah+bh {
			size = max(ah, min(size, available-bh))
		} else {
			size = max(0, min(size, available))
		}
		a.h = size
		b.y = r.y + size + 1
		b.h = available - size
	}
	n.first.place(a, out)
	n.second.place(b, out)
}

func (n *layout) find(id int) *layout {
	if n.id == id {
		return n
	}
	if n.id != 0 {
		return nil
	}
	if found := n.first.find(id); found != nil {
		return found
	}
	return n.second.find(id)
}

func (n *layout) contains(id int) bool { return n.find(id) != nil }

func (n *layout) firstPane() int {
	if n.id != 0 {
		return n.id
	}
	return n.first.firstPane()
}

// remove collapses a pane's parent into its surviving sibling. The returned
// pane ID is a deterministic neighboring focus target, not an arbitrary map entry.
func (n *layout) remove(id int) (*layout, int) {
	if n.id != 0 {
		if n.id == id {
			return nil, 0
		}
		return n, 0
	}
	if n.first.contains(id) {
		child, neighbor := n.first.remove(id)
		if child == nil {
			return n.second, n.second.firstPane()
		}
		n.first = child
		return n, neighbor
	}
	if n.second.contains(id) {
		child, neighbor := n.second.remove(id)
		if child == nil {
			return n.first, n.first.firstPane()
		}
		n.second = child
		return n, neighbor
	}
	return n, 0
}

// resize adjusts the nearest divider on the requested edge. A positive delta
// moves that divider right/down, matching tmux's resize-pane direction semantics.
func (n *layout) resize(id int, direction string, delta int, r rect) bool {
	if n.id != 0 {
		return false
	}
	aw, ah := n.first.minimum()
	bw, bh := n.second.minimum()
	a, b := r, r
	if n.direction == "horizontal" {
		available := r.w - 1
		if available < aw+bw {
			return false
		}
		size := max(aw, min(available*n.ratio/10000, available-bw))
		a.w = size
		b.x += size + 1
		b.w = available - size
	} else {
		available := r.h - 1
		if available < ah+bh {
			return false
		}
		size := max(ah, min(available*n.ratio/10000, available-bh))
		a.h = size
		b.y += size + 1
		b.h = available - size
	}
	inFirst := n.first.contains(id)
	child, childRect := n.second, b
	if inFirst {
		child, childRect = n.first, a
	}
	if child.resize(id, direction, delta, childRect) {
		return true
	}
	horizontal := direction == "left" || direction == "right"
	if horizontal != (n.direction == "horizontal") {
		return false
	}
	available, size, lo, other := r.h-1, a.h, ah, bh
	if horizontal {
		available, size, lo, other = r.w-1, a.w, aw, bw
	}
	if direction == "left" || direction == "up" {
		delta = -delta
	}
	size = max(lo, min(size+delta, available-other))
	// Round up so place() reproduces the exact requested number of cells.
	n.ratio = (size*10000 + available - 1) / available
	return true
}

// setDivider moves the divider that separates panes first and second, which
// sit on its left/top and right/bottom, to column (horizontal split) or row
// (vertical split) at, within the minimum pane sizes. That divider is the
// split where the two panes' subtrees meet, so it keeps its identity while
// other parts of the layout change. It reports false when the panes don't
// meet across a split in that direction or there's no room to move it.
func (n *layout) setDivider(first, second int, direction string, at int, r rect) bool {
	if n.id != 0 {
		return false
	}
	aw, ah := n.first.minimum()
	bw, bh := n.second.minimum()
	a, b := r, r
	available, origin, lo, other := max(0, r.h-1), r.y, ah, bh
	if n.direction == "horizontal" {
		available, origin, lo, other = max(0, r.w-1), r.x, aw, bw
	}
	// The same geometry as place(), so child rectangles match what's shown.
	size := available * n.ratio / 10000
	if available >= lo+other {
		size = max(lo, min(size, available-other))
	} else {
		size = max(0, min(size, available))
	}
	if n.direction == "horizontal" {
		a.w, b.x, b.w = size, r.x+size+1, available-size
	} else {
		a.h, b.y, b.h = size, r.y+size+1, available-size
	}
	inFirst, inSecond := n.first.contains(first), n.second.contains(second)
	switch {
	case inFirst && n.first.contains(second):
		return n.first.setDivider(first, second, direction, at, a)
	case inSecond && n.second.contains(first):
		return n.second.setDivider(first, second, direction, at, b)
	case !inFirst || !inSecond || n.direction != direction || available < lo+other:
		return false
	}
	size = max(lo, min(at-origin, available-other))
	// Round up so place() reproduces the exact requested number of cells.
	n.ratio = (size*10000 + available - 1) / available
	return true
}
