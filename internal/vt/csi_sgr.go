package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// handleSgr handles Select Graphic Rendition (SGR) escape sequences.
func (e *Emulator) handleSgr(params ansi.Params) {
	uv.ReadStyle(params, &e.scr.cur.Pen)
	e.scr.syncCursorStyle()
	e.compactStylesIfNeeded()
}
