package client

import (
	"github.com/gdamore/tcell/v2"

	"github.com/josephferrero/bmux/internal/protocol"
)

// paneDivider records only reserved layout cells, not terminal content. Arrows
// are derived from the panes immediately touching each segment, so a divider
// shared by three panes cannot falsely identify both right-hand panes as focused.
type paneDivider struct {
	vertical, horizontal bool
	arrow                rune
}

// drawPaneDividers must run after every drawPane call. No chrome is permitted
// inside a pane rectangle, even if that terminal has no rows or has exited.
func (u *ui) drawPaneDividers(width, height int) {
	u.drawDividers(u.workspace, pickerArea{0, paneOriginY, width, max(0, height-1-paneOriginY)})
}

func (u *ui) drawDividers(frame *protocol.Frame, area pickerArea) {
	if frame == nil {
		return
	}
	panes := frame.Panes
	dividers := make(map[point]paneDivider)
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
	for _, p := range panes {
		if p.Width < 1 || p.Height < 1 {
			continue
		}
		for y := max(0, p.Y); y < min(p.Y+p.Height, area.h); y++ {
			add(p.X+p.Width, y, true)
		}
		for x := max(0, p.X); x < min(p.X+p.Width, area.w); x++ {
			add(x, p.Y+p.Height, false)
		}
	}
	// A T-junction or crossing cell belongs to no pane edge, so the border is
	// interrupted exactly where segments meet. A cell continues a divider when
	// neighbors on both sides carry that orientation; two passes also cover
	// cells whose own continuity depends on another filled cell. Segment
	// ownership stays pane-derived: only genuinely uncovered cells fill in.
	for pass := 0; pass < 2; pass++ {
		for y := 0; y < area.h; y++ {
			for x := 0; x < area.w; x++ {
				at := point{x, y}
				if _, ok := dividers[at]; ok {
					continue
				}
				if coveredByPane(x, y) {
					continue
				}
				var d paneDivider
				if dividers[point{x, y - 1}].vertical && dividers[point{x, y + 1}].vertical {
					d.vertical = true
				}
				if dividers[point{x - 1, y}].horizontal && dividers[point{x + 1, y}].horizontal {
					d.horizontal = true
				}
				if d.vertical || d.horizontal {
					dividers[at] = d
				}
			}
		}
	}
	// Focus attribution runs over the final cell set, junction cells included.
	arrows := make(map[point]rune, len(dividers))
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
	markers := make(map[point]rune)
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
		for _, n := range []struct {
			at       point
			vertical bool
		}{{point{at.x, at.y - 1}, true}, {point{at.x, at.y + 1}, true}, {point{at.x - 1, at.y}, false}, {point{at.x + 1, at.y}, false}} {
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
			text = string(marker)
		}
		u.screen.Put(area.x+at.x, area.y+at.y, text, style)
	}
}

var dividerStyle = tcell.StyleDefault.Foreground(chrome.muted)

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
