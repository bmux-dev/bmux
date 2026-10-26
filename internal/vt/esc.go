package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// handleEsc handles an escape sequence.
func (e *Emulator) handleEsc(cmd ansi.Cmd) {
	e.flushGrapheme() // Flush any pending grapheme before handling ESC sequences.
	if !e.handlers.handleEsc(int(cmd)) {
		var str string
		if inter := cmd.Intermediate(); inter != 0 {
			str += string(inter) + " "
		}
		if final := cmd.Final(); final != 0 {
			str += string(final)
		}
		e.logf("unhandled sequence: ESC %q", str)
	}
}

// fullReset performs a full terminal reset as in [ansi.RIS].
func (e *Emulator) fullReset() {
	e.scrs[0].Reset()
	e.scrs[1].Reset()
	e.resetTabStops()

	// XXX: Do we reset all modes here? Investigate.
	e.resetModes()
	e.resetKeyboard()

	e.gl, e.gr = 0, 1
	e.gsingle = 0
	e.charsets = [4]CharSet{}
	e.atPhantom = false
	e.grapheme = e.grapheme[:0]
	e.lastChar = 0
}

// softReset performs a soft terminal reset as in DECSTR, which the reset
// command sends. Like xterm, it leaves the screen, the cursor position and
// autowrap alone, and turns off left and right margins.
func (e *Emulator) softReset() {
	for mode, setting := range map[ansi.Mode]ansi.ModeSetting{
		ansi.ModeTextCursorEnable: ansi.ModeSet,
		ansi.ModeInsertReplace:    ansi.ModeReset,
		ansi.ModeOrigin:           ansi.ModeReset,
		ansi.ModeCursorKeys:       ansi.ModeReset,
		ansi.ModeNumericKeypad:    ansi.ModeReset,
		ansi.ModeLeftRightMargin:  ansi.ModeReset,
	} {
		e.setMode(mode, setting)
	}
	e.scr.setVerticalMargins(0, e.Height())
	e.scr.setHorizontalMargins(0, e.Width())
	e.scr.cur.Pen, e.scr.cur.Link = uv.Style{}, uv.Link{}
	e.scr.saved.Position, e.scr.saved.Pen, e.scr.saved.Link = uv.Position{}, uv.Style{}, uv.Link{}
	e.gl, e.gr = 0, 1
	e.gsingle = 0
	e.charsets = [4]CharSet{}
	e.atPhantom = false
}
