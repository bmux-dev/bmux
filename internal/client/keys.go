package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
	"unicode"
)

func keyRequest(e *tcell.EventKey) protocol.Request {
	r := protocol.Request{Op: "key"}
	if e.Modifiers()&tcell.ModAlt != 0 {
		r.Mod |= int(uv.ModAlt)
	}
	if e.Modifiers()&tcell.ModShift != 0 {
		r.Mod |= int(uv.ModShift)
	}
	if e.Modifiers()&tcell.ModCtrl != 0 {
		r.Mod |= int(uv.ModCtrl)
	}
	if e.Key() == tcell.KeyRune {
		r.Code, r.Text = e.Rune(), string(e.Rune())
		if e.Modifiers()&tcell.ModCtrl != 0 {
			r.Text = ""
			// Legacy terminals don't distinguish Ctrl-letter from
			// Ctrl-Shift-letter; x/vt doesn't implement extended keys yet.
			if unicode.IsLetter(r.Code) {
				r.Code = unicode.ToLower(r.Code)
				r.Mod &^= int(uv.ModShift)
			}
		}
		return r
	}
	keys := map[tcell.Key]rune{
		tcell.KeyEnter: uv.KeyEnter, tcell.KeyTab: uv.KeyTab,
		tcell.KeyEscape: uv.KeyEscape, tcell.KeyBackspace: uv.KeyBackspace, tcell.KeyBackspace2: uv.KeyBackspace,
		tcell.KeyUp: uv.KeyUp, tcell.KeyDown: uv.KeyDown, tcell.KeyLeft: uv.KeyLeft, tcell.KeyRight: uv.KeyRight,
		tcell.KeyHome: uv.KeyHome, tcell.KeyEnd: uv.KeyEnd, tcell.KeyPgUp: uv.KeyPgUp, tcell.KeyPgDn: uv.KeyPgDown,
		tcell.KeyInsert: uv.KeyInsert, tcell.KeyDelete: uv.KeyDelete,
		tcell.KeyBacktab: uv.KeyTab,
	}
	if code, ok := keys[e.Key()]; ok {
		r.Code = code
		if e.Key() == tcell.KeyBacktab {
			r.Mod |= int(uv.ModShift)
		}
	} else if e.Key() >= tcell.KeyF1 && e.Key() <= tcell.KeyF64 {
		r.Code = uv.KeyF1 + rune(e.Key()-tcell.KeyF1)
	} else if e.Key() >= tcell.KeyCtrlA && e.Key() <= tcell.KeyCtrlZ {
		r.Code, r.Mod = 'a'+rune(e.Key()-tcell.KeyCtrlA), r.Mod|int(uv.ModCtrl)
	} else {
		switch e.Key() {
		case tcell.KeyCtrlSpace:
			r.Code, r.Mod = ' ', r.Mod|int(uv.ModCtrl)
		case tcell.KeyCtrlBackslash:
			r.Code, r.Mod = '\\', r.Mod|int(uv.ModCtrl)
		case tcell.KeyCtrlRightSq:
			r.Code, r.Mod = ']', r.Mod|int(uv.ModCtrl)
		case tcell.KeyCtrlCarat:
			r.Code, r.Mod = '^', r.Mod|int(uv.ModCtrl)
		case tcell.KeyCtrlUnderscore:
			r.Code, r.Mod = '_', r.Mod|int(uv.ModCtrl)
		}
	}
	return r
}
