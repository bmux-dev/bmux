package client

import "github.com/josephferrero/bmux/internal/protocol"

// dividerDrag is a mouse drag moving the divider between two panes. The pane
// pair identifies the divider on the server, so it survives the layout
// changing under the drag.
type dividerDrag struct {
	first, second int    // panes on the divider's left/top and right/bottom
	direction     string // "horizontal" for side-by-side panes, else "vertical"
	at            int    // pane-area column or row last requested
}

// dividerAt finds the divider under pane-area cell (x, y) and the panes on
// either side. Where dividers meet, a pane one row or column away counts too,
// and the divider with more panes touching the cell itself wins: that's the
// one running through the junction rather than ending at it.
func (u *ui) dividerAt(x, y int) (dividerDrag, bool) {
	if u.workspace == nil || u.workspace.Zoomed {
		return dividerDrag{}, false
	}
	panes := u.workspace.Panes
	for _, p := range panes {
		if x >= p.X && x < p.X+p.Width && y >= p.Y && y < p.Y+p.Height {
			return dividerDrag{}, false
		}
	}
	// find returns the first matching pane and whether it touches the cell
	// itself rather than a neighboring row or column.
	find := func(match func(p protocol.PaneFrame, offset int) bool) (int, bool) {
		for _, offset := range []int{0, -1, 1} {
			for _, p := range panes {
				if match(p, offset) {
					return p.ID, offset == 0
				}
			}
		}
		return 0, false
	}
	rows := func(p protocol.PaneFrame, offset int) bool { return y+offset >= p.Y && y+offset < p.Y+p.Height }
	columns := func(p protocol.PaneFrame, offset int) bool { return x+offset >= p.X && x+offset < p.X+p.Width }
	exact := func(a, b bool) (n int) {
		if a {
			n++
		}
		if b {
			n++
		}
		return n
	}
	left, leftExact := find(func(p protocol.PaneFrame, o int) bool { return p.X+p.Width == x && rows(p, o) })
	right, rightExact := find(func(p protocol.PaneFrame, o int) bool { return p.X == x+1 && rows(p, o) })
	top, topExact := find(func(p protocol.PaneFrame, o int) bool { return p.Y+p.Height == y && columns(p, o) })
	bottom, bottomExact := find(func(p protocol.PaneFrame, o int) bool { return p.Y == y+1 && columns(p, o) })
	sideBySide := left != 0 && right != 0
	stacked := top != 0 && bottom != 0
	if stacked && (!sideBySide || exact(topExact, bottomExact) > exact(leftExact, rightExact)) {
		return dividerDrag{first: top, second: bottom, direction: "vertical", at: y}, true
	}
	if sideBySide {
		return dividerDrag{first: left, second: right, direction: "horizontal", at: x}, true
	}
	return dividerDrag{}, false
}

// dragDivider follows the pointer to pane-area cell (x, y), asking the server
// to move the divider only when the pointer reaches another column or row.
func (u *ui) dragDivider(x, y int) {
	d := u.divider
	r := protocol.Request{Op: "drag-divider", PaneID: d.first, NeighborID: d.second, Direction: d.direction}
	at := x
	if d.direction == "vertical" {
		at = y
	}
	if at == d.at {
		return
	}
	d.at = at
	if d.direction == "vertical" {
		r.Y = at
	} else {
		r.X = at
	}
	u.send(r)
}
