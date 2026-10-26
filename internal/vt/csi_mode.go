package vt

import (
	"io"

	"github.com/charmbracelet/x/ansi"
)

func (e *Emulator) handleMode(params ansi.Params, set, isAnsi bool) {
	for _, p := range params {
		param := p.Param(-1)
		if param == -1 {
			// Missing parameter, ignore
			continue
		}

		var mode ansi.Mode = ansi.DECMode(param)
		if isAnsi {
			mode = ansi.ANSIMode(param)
		}

		setting := e.modes[mode]
		if setting == ansi.ModePermanentlyReset || setting == ansi.ModePermanentlySet {
			// Permanently set modes are ignored.
			continue
		}

		setting = ansi.ModeReset
		if set {
			setting = ansi.ModeSet
		}

		e.setMode(mode, setting)
	}
}

// modeAltScreenLegacy is DECSET 47, the original alternate screen switch.
const modeAltScreenLegacy = ansi.DECMode(47)

// setAltScreenMode switches to or from the alternate screen, erasing it on
// the way in when clear is set (1047 and 1049, but not 47).
func (e *Emulator) setAltScreenMode(on, clear bool) {
	if (on && e.scr == &e.scrs[1]) || (!on && e.scr == &e.scrs[0]) {
		// Already in alternate screen mode, or normal screen, do nothing.
		return
	}
	if on {
		e.kitty[1] = kittyKeyboard{}
		e.scr = &e.scrs[1]
		e.scrs[1].cur = e.scrs[0].cur
		if clear {
			e.scr.Clear()
			e.scr.ClearTouched()
		}
	} else {
		e.scr = &e.scrs[0]
	}
	if e.cb.AltScreen != nil {
		e.cb.AltScreen(on)
	}
	if e.cb.CursorVisibility != nil {
		e.cb.CursorVisibility(!e.scr.cur.Hidden)
	}
}

// saveCursor saves the cursor position.
func (e *Emulator) saveCursor() {
	e.scr.SaveCursor()
}

// restoreCursor restores the cursor position.
func (e *Emulator) restoreCursor() {
	e.scr.RestoreCursor()
}

// setMode sets the mode to the given value.
func (e *Emulator) setMode(mode ansi.Mode, setting ansi.ModeSetting) {
	e.logf("setting mode %T(%v) to %v", mode, mode, setting)
	e.modes[mode] = setting
	switch mode {
	case ansi.ModeTextCursorEnable:
		e.scr.setCursorHidden(!setting.IsSet())
	case ansi.ModeInsertReplace:
		e.insert = setting.IsSet()
	case ansi.ModeOrigin:
		// Home: the margins' top-left when set, the screen's when reset.
		e.setCursorPosition(0, 0)
	case ansi.ModeLeftRightMargin:
		if !setting.IsSet() {
			e.scr.setHorizontalMargins(0, e.Width())
		}
	case ansi.ModeAltScreen, modeAltScreenLegacy:
		// Like xterm, 47 and 1047 keep the cursor where it is both ways.
		pos := e.scr.cur.Position
		e.setAltScreenMode(setting.IsSet(), mode == ansi.ModeAltScreen)
		e.scr.setCursor(pos.X, pos.Y, false)
	case ansi.ModeSaveCursor:
		if setting.IsSet() {
			e.saveCursor()
		} else {
			e.restoreCursor()
		}
	case ansi.ModeAltScreenSaveCursor:
		// Save the main screen's cursor, then enter a blank alternate screen
		// with the cursor at home. Leaving returns to the main screen's cursor.
		entering := setting.IsSet() && !e.IsAltScreen()
		if setting.IsSet() {
			e.saveCursor()
		}
		e.setAltScreenMode(setting.IsSet(), true)
		if entering {
			e.setCursor(0, 0)
		}
	case ansi.ModeInBandResize:
		if setting.IsSet() {
			pw, ph := e.pixelSize()
			_, _ = io.WriteString(e.pw, ansi.InBandResize(e.Height(), e.Width(), ph, pw))
		}
	}
	if setting.IsSet() {
		if e.cb.EnableMode != nil {
			e.cb.EnableMode(mode)
		}
	} else if setting.IsReset() {
		if e.cb.DisableMode != nil {
			e.cb.DisableMode(mode)
		}
	}
}

// isModeSet returns true if the mode is set.
func (e *Emulator) isModeSet(mode ansi.Mode) bool {
	m, ok := e.modes[mode]
	return ok && m.IsSet()
}
