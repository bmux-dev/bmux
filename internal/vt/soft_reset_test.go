package vt

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// DECSTR, which the reset command sends, restores modes, margins, the pen and
// the saved cursor, but keeps the screen and the cursor position.
func TestSoftResetRestoresState(t *testing.T) {
	term := newTestTerminal(t, 10, 6)
	term.WriteString("hi" +
		"\x1b[3;5H\x1b[1m\x1b7" + // save the cursor at row 3, column 5, with bold
		"\x1b[?69h\x1b[2;6s" + // left and right margins
		"\x1b[2;4r\x1b[?6h\x1b[4h\x1b[?1h\x1b[?25l" + // margins, origin, insert, cursor keys, hidden
		"\x1b[2;3H" + // row 2, column 3 of the region: screen row 3, column 4
		"\x1b[!p")

	if pos := term.CursorPosition(); pos != uv.Pos(3, 2) {
		t.Fatalf("DECSTR moved the cursor to %v; want it kept at 3,2", pos)
	}
	if got := term.CellAt(0, 0).Content + term.CellAt(1, 0).Content; got != "hi" {
		t.Fatalf("DECSTR changed the screen: row 1 starts %q", got)
	}
	for _, m := range []ansi.Mode{ansi.ModeOrigin, ansi.ModeInsertReplace, ansi.ModeCursorKeys, ansi.ModeLeftRightMargin} {
		if term.isModeSet(m) {
			t.Errorf("mode %v still set after DECSTR", m)
		}
	}
	if !term.isModeSet(ansi.ModeTextCursorEnable) {
		t.Error("the cursor is still hidden after DECSTR")
	}
	if got, want := term.scr.ScrollRegion(), (uv.Rect(0, 0, 10, 6)); got != want {
		t.Errorf("scroll region %v after DECSTR, want the whole screen %v", got, want)
	}
	if !term.scr.cur.Pen.IsZero() {
		t.Errorf("pen %+v after DECSTR, want plain", term.scr.cur.Pen)
	}

	// Origin mode is off, so CUP is absolute; the saved cursor is home and plain.
	term.WriteString("\x1b[6;10H\x1b8")
	if pos := term.CursorPosition(); pos != uv.Pos(0, 0) {
		t.Errorf("DECRC after DECSTR went to %v, want home", pos)
	}
	if !term.scr.cur.Pen.IsZero() {
		t.Errorf("DECRC after DECSTR restored pen %+v, want plain", term.scr.cur.Pen)
	}
}
