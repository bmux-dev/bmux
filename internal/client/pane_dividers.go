package client

import (
	"github.com/bmux-dev/bmux/internal/protocol"
	"github.com/gdamore/tcell/v2"
)

// paneDivider records only reserved layout cells, not terminal content. Arrows
// are derived from the panes immediately touching each segment, so a divider
// shared by three panes cannot falsely identify both right-hand panes as focused.
type paneDivider struct {
	vertical, horizontal bool
	arrow                rune
}

// dividerScratch holds reusable maps and slices for drawDividers so multi-pane
// frames do not reallocate them on every redraw.
type dividerScratch struct {
	cells      map[point]paneDivider
	arrows     map[point]rune
	markers    map[point]rune
	candidates []point
	pass2      []point
	seen       map[point]bool
}

func (u *ui) dividerBuf() *dividerScratch {
	if u.dividers == nil {
		u.dividers = &dividerScratch{
			cells:   make(map[point]paneDivider),
			arrows:  make(map[point]rune),
			markers: make(map[point]rune),
			seen:    make(map[point]bool),
		}
	}
	return u.dividers
}

// drawPaneDividers must run after every drawPane call. No chrome is permitted
// inside a pane rectangle, even if that terminal has no rows or has exited.
func (u *ui) drawPaneDividers(width, height int) {
	if u.workspace == nil || len(u.workspace.Panes) <= 1 {
		if u.dividers != nil {
			clear(u.dividers.cells)
		}
		return
	}
	x := u.paneOriginX(width)
	u.drawDividers(u.workspace, pickerArea{x, paneOriginY, max(0, width-x), max(0, height-1-paneOriginY)})
}

func (u *ui) drawDividers(frame *protocol.Frame, area pickerArea) {
	if frame == nil || len(frame.Panes) <= 1 || area.w <= 0 || area.h <= 0 {
		if u.dividers != nil {
			clear(u.dividers.cells)
		}
		return
	}
	panes := frame.Panes
	buf := u.dividerBuf()
	dividers := buf.cells
	arrows := buf.arrows
	markers := buf.markers
	seen := buf.seen
	clear(dividers)
	clear(arrows)
	clear(markers)
	clear(seen)
	candidates := buf.candidates[:0]
	pass2 := buf.pass2[:0]

	coveredByPane := func(x, y int) bool {
		for _, p := range panes {
			if x >= p.X && x < p.X+p.Width && y >= p.Y && y < p.Y+p.Height {
				return true
			}
		}
		return false
	}
	add := func(x, y int, vertical bool) {
		if x < 0 || x >= area.w || y < 0 || y >= area.h {
			return
		}
		if coveredByPane(x, y) {
			return
		}
		at := point{x, y}
		d := dividers[at]
		if vertical {
			d.vertical = true
		} else {
			d.horizontal = true
		}
		dividers[at] = d
	}
	addCandidate := func(x, y int, dst *[]point) {
		if x < 0 || x >= area.w || y < 0 || y >= area.h {
			return
		}
		at := point{x, y}
		if seen[at] {
			return
		}
		seen[at] = true
		*dst = append(*dst, at)
	}
	for _, p := range panes {
		if p.Width < 1 || p.Height < 1 {
			continue
		}
		rx := p.X + p.Width
		if rx >= 0 && rx < area.w {
			for y := max(0, p.Y); y < min(p.Y+p.Height, area.h); y++ {
				add(rx, y, true)
			}
		}
		by := p.Y + p.Height
		if by >= 0 && by < area.h {
			for x := max(0, p.X); x < min(p.X+p.Width, area.w); x++ {
				add(x, by, false)
			}
		}
		// Unowned T-junction and crossing cells sit at pane border endpoints.
		addCandidate(rx, by, &candidates)
		addCandidate(rx, p.Y-1, &candidates)
		addCandidate(p.X-1, by, &candidates)
		addCandidate(p.X-1, p.Y-1, &candidates)
	}
	// A T-junction or crossing cell belongs to no pane edge, so the border is
	// interrupted exactly where segments meet. A cell continues a divider when
	// neighbors on both sides carry that orientation; two passes over candidate
	// endpoints and their neighbors cover cells whose own continuity depends on
	// another filled cell without scanning the full terminal grid.
	tryFill := func(at point) bool {
		if _, ok := dividers[at]; ok {
			return false
		}
		if coveredByPane(at.x, at.y) {
			return false
		}
		var d paneDivider
		if dividers[point{at.x, at.y - 1}].vertical && dividers[point{at.x, at.y + 1}].vertical {
			d.vertical = true
		}
		if dividers[point{at.x - 1, at.y}].horizontal && dividers[point{at.x + 1, at.y}].horizontal {
			d.horizontal = true
		}
		if d.vertical || d.horizontal {
			dividers[at] = d
			return true
		}
		return false
	}
	for _, at := range candidates {
		if tryFill(at) {
			addCandidate(at.x, at.y-1, &pass2)
			addCandidate(at.x, at.y+1, &pass2)
			addCandidate(at.x-1, at.y, &pass2)
			addCandidate(at.x+1, at.y, &pass2)
		}
	}
	for _, at := range candidates {
		tryFill(at)
	}
	for _, at := range pass2 {
		tryFill(at)
	}
	buf.candidates = candidates[:0]
	buf.pass2 = pass2[:0]

	// Focus attribution runs over the final cell set, junction cells included.
	for at, d := range dividers {
		for _, p := range panes {
			if p.ID != frame.Focused || p.Width < 1 || p.Height < 1 {
				continue
			}
			if d.vertical && at.y >= p.Y && at.y < p.Y+p.Height {
				if at.x == p.X-1 {
					d.arrow = '>'
				} else if at.x == p.X+p.Width {
					d.arrow = '<'
				}
			} else if d.horizontal && !d.vertical && at.x >= p.X && at.x < p.X+p.Width {
				if at.y == p.Y-1 {
					d.arrow = 'v'
				} else if at.y == p.Y+p.Height {
					d.arrow = '^'
				}
			}
		}
		if d.arrow != 0 {
			arrows[at] = d.arrow
		}
		dividers[at] = d
	}
	// A marker at the midpoint of each uninterrupted ownership segment remains
	// legible in monochrome and in one-cell splits. Never place it on a junction.
	for at, d := range dividers {
		if d.arrow == 0 {
			continue
		}
		step := point{1, 0}
		if d.arrow == '<' || d.arrow == '>' {
			step = point{0, 1}
		}
		isSegment := func(p point) bool {
			candidate, ok := dividers[p]
			if !ok || candidate.arrow != d.arrow || candidate.vertical != d.vertical || candidate.horizontal != d.horizontal {
				return false
			}
			// The other orientation can end next to a T-junction.
			if step.y == 1 {
				return !dividers[point{p.x - 1, p.y}].horizontal && !dividers[point{p.x + 1, p.y}].horizontal
			}
			return !dividers[point{p.x, p.y - 1}].vertical && !dividers[point{p.x, p.y + 1}].vertical
		}
		if !isSegment(at) || isSegment(point{at.x - step.x, at.y - step.y}) {
			continue
		}
		length := 1
		for isSegment(point{at.x + length*step.x, at.y + length*step.y}) {
			length++
		}
		markers[point{at.x + (length-1)/2*step.x, at.y + (length-1)/2*step.y}] = d.arrow
	}
	// The focus cue covers only the focused pane's own edges. A junction where
	// one of those edges ends takes the cue too, so the highlighted border
	// meets its corner; lines beyond the junction keep their own style.
	for at, d := range dividers {
		if d.arrow != 0 {
			continue
		}
		if glyph := dividerGlyph(dividers, at, d); glyph == "│" || glyph == "─" {
			continue
		}
		neighbors := [4]struct {
			at       point
			vertical bool
		}{{point{at.x, at.y - 1}, true}, {point{at.x, at.y + 1}, true}, {point{at.x - 1, at.y}, false}, {point{at.x + 1, at.y}, false}}
		for _, n := range neighbors {
			neighbor := dividers[n.at]
			if arrow := arrows[n.at]; arrow != 0 && (n.vertical && neighbor.vertical || !n.vertical && neighbor.horizontal) {
				d.arrow = arrow
				break
			}
		}
		dividers[at] = d
	}
	for at, d := range dividers {
		// Dividers sit on the terminal's own background, like pane cells, so
		// the line does not carry a band of chrome colour beside it.
		style := dividerStyle
		text := dividerGlyph(dividers, at, d)
		if d.arrow != 0 {
			style = style.Foreground(accentColor).Bold(true)
			// Junctions take the heavy form too, so an emphasized line keeps
			// one stroke weight through the joint.
			if heavy, ok := heavyGlyph[text]; ok {
				text = heavy
			}
		}
		if marker := markers[at]; marker != 0 {
			text = arrowString(marker)
		}
		u.screen.Put(area.x+at.x, area.y+at.y, text, style)
	}
}

func arrowString(r rune) string {
	switch r {
	case '<':
		return "<"
	case '>':
		return ">"
	case '^':
		return "^"
	case 'v':
		return "v"
	default:
		return string(r)
	}
}

// fillRect writes spaces in style across [x, x+w) x [y, y+h). Using Put
// preserves tcell's differential cell state when cells are already blank.
func (u *ui) fillRect(x, y, w, h int, style tcell.Style) {
	if w <= 0 || h <= 0 {
		return
	}
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			u.screen.Put(col, row, " ", style)
		}
	}
}

// fillOutsideRect fills every cell in outer that lies outside inner with spaces
// in style, without touching cells inside inner.
func (u *ui) fillOutsideRect(outer, inner pickerArea, style tcell.Style) {
	if outer.w <= 0 || outer.h <= 0 {
		return
	}
	ix0 := max(outer.x, inner.x)
	iy0 := max(outer.y, inner.y)
	ix1 := min(outer.x+outer.w, inner.x+inner.w)
	iy1 := min(outer.y+outer.h, inner.y+inner.h)
	if ix0 >= ix1 || iy0 >= iy1 {
		u.fillRect(outer.x, outer.y, outer.w, outer.h, style)
		return
	}
	if ix0 == outer.x && iy0 == outer.y && ix1 == outer.x+outer.w && iy1 == outer.y+outer.h {
		return
	}
	u.fillRect(outer.x, outer.y, outer.w, iy0-outer.y, style)
	u.fillRect(outer.x, iy1, outer.w, outer.y+outer.h-iy1, style)
	u.fillRect(outer.x, iy0, ix0-outer.x, iy1-iy0, style)
	u.fillRect(ix1, iy0, outer.x+outer.w-ix1, iy1-iy0, style)
}

// fillUncovered paints any cells in area not covered by frame's panes or the
// dividers drawn for frame with spaces in style. Must run right after
// drawDividers(frame, area).
func (u *ui) fillUncovered(frame *protocol.Frame, area pickerArea, style tcell.Style) {
	if area.w <= 0 || area.h <= 0 {
		return
	}
	if frame == nil || len(frame.Panes) == 0 {
		u.fillRect(area.x, area.y, area.w, area.h, style)
		return
	}
	panes := frame.Panes
	if len(panes) == 1 {
		p := panes[0]
		u.fillOutsideRect(area, pickerArea{area.x + p.X, area.y + p.Y, p.Width, p.Height}, style)
		return
	}
	disjoint := true
	covered := 0
	for i, p := range panes {
		x0, y0 := max(0, p.X), max(0, p.Y)
		x1, y1 := min(area.w, p.X+p.Width), min(area.h, p.Y+p.Height)
		if x0 >= x1 || y0 >= y1 {
			continue
		}
		covered += (x1 - x0) * (y1 - y0)
		for j := i + 1; j < len(panes) && disjoint; j++ {
			q := panes[j]
			qx0, qy0 := max(0, q.X), max(0, q.Y)
			qx1, qy1 := min(area.w, q.X+q.Width), min(area.h, q.Y+q.Height)
			if qx0 < qx1 && qy0 < qy1 && x0 < qx1 && qx0 < x1 && y0 < qy1 && qy0 < y1 {
				disjoint = false
			}
		}
	}
	var dividers map[point]paneDivider
	if u.dividers != nil {
		dividers = u.dividers.cells
	}
	if disjoint && covered+len(dividers) == area.w*area.h {
		return
	}
	for y := 0; y < area.h; y++ {
		for x := 0; x < area.w; x++ {
			if _, ok := dividers[point{x, y}]; ok {
				continue
			}
			inPane := false
			for _, p := range panes {
				if x >= p.X && x < p.X+p.Width && y >= p.Y && y < p.Y+p.Height {
					inPane = true
					break
				}
			}
			if !inPane {
				u.screen.Put(area.x+x, area.y+y, " ", style)
			}
		}
	}
}

var heavyGlyph = map[string]string{
	"│": "┃", "─": "━", "┼": "╋", "┤": "┫", "├": "┣", "┬": "┳", "┴": "┻",
	"┘": "┛", "└": "┗", "┐": "┓", "┌": "┏",
}

// dividerGlyph picks tmux-style box glyphs from the surrounding segments so
// lines read as continuously drawn, including one-sided stubs where a segment
// abuts another without a crossing cell: the arm belongs on this cell.
func dividerGlyph(dividers map[point]paneDivider, at point, d paneDivider) string {
	up := dividers[point{at.x, at.y - 1}].vertical
	down := dividers[point{at.x, at.y + 1}].vertical
	left := dividers[point{at.x - 1, at.y}].horizontal
	right := dividers[point{at.x + 1, at.y}].horizontal
	switch {
	case up && down && left && right:
		return "┼"
	case up && down && left:
		return "┤"
	case up && down && right:
		return "├"
	case left && right && down:
		return "┬"
	case left && right && up:
		return "┴"
	case up && left:
		return "┘"
	case up && right:
		return "└"
	case down && left:
		return "┐"
	case down && right:
		return "┌"
	}
	if d.vertical {
		return "│"
	}
	return "─"
}
