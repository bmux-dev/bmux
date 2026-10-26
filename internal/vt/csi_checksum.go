package vt

import (
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// requestChecksum answers DECRQCRA, Request Checksum of Rectangular Area:
// CSI Pid ; Pp ; Pt ; Pl ; Pb ; Pr * y. The reply, DCS Pid ! ~ XXXX ST, is
// the 16-bit two's complement of the sum of the rectangle's character codes,
// as DEC terminals send it; a blank cell counts as a space. Conformance
// suites such as esctest and vttest read the screen this way. A program sees
// only its own pane, so this reveals nothing it couldn't already know.
func (e *Emulator) requestChecksum(params ansi.Params) {
	id, _, _ := params.Param(0, 0)
	width, height := e.Width(), e.Height()
	top, _, _ := params.Param(2, 1)
	left, _, _ := params.Param(3, 1)
	bottom, _, _ := params.Param(4, height)
	right, _, _ := params.Param(5, width)
	top, left = max(top, 1), max(left, 1)
	if bottom < 1 {
		bottom = height
	}
	if right < 1 {
		right = width
	}
	// Origin mode makes coordinates relative to the margins, and limits the
	// rectangle to them.
	minY, maxY, minX, maxX := 0, height, 0, width
	if e.isModeSet(ansi.DECOM) {
		scroll := e.scr.ScrollRegion()
		minY, maxY, minX, maxX = scroll.Min.Y, scroll.Max.Y, scroll.Min.X, scroll.Max.X
	}
	var sum uint16
	for y := max(minY+top-1, minY); y < min(minY+bottom, maxY); y++ {
		for x := max(minX+left-1, minX); x < min(minX+right, maxX); x++ {
			c := e.CellAt(x, y)
			if c == nil || c.Width == 0 {
				continue // the right half of a wide character
			}
			r := ' '
			if c.Content != "" {
				r, _ = utf8.DecodeRuneInString(c.Content)
			}
			sum += uint16(r)
		}
	}
	_, _ = io.WriteString(e.pw, fmt.Sprintf("\x1bP%d!~%04X\x1b\\", id, uint16(0x10000-int(sum))))
}
