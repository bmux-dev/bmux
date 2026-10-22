package client

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

type modalLine struct {
	text  string
	style tcell.Style
}

// modalFrame dims everything between the bars and draws an empty titled box
// big enough for contentWidth columns and contentLines lines, shrunk to fit
// the screen. It returns where the content starts and how much of it shows;
// ok is false when the screen is too small for any box.
func (u *ui) modalFrame(w, h int, title string, contentWidth, contentLines int) (x, y, inner, rows int, ok bool) {
	return u.modalBox(w, h, title, contentWidth, contentLines, true)
}

// modalBox is modalFrame with the choice of dimming the body. Leave it
// undimmed when the user needs to judge the colors behind the box.
func (u *ui) modalBox(w, h int, title string, contentWidth, contentLines int, dim bool) (x, y, inner, rows int, ok bool) {
	u.screen.HideCursor()
	if w < 10 || h < 6 {
		return 0, 0, 0, 0, false
	}
	if dim {
		u.dimBody(w, h)
	}
	width := min(max(contentWidth, displayWidth(title)+2)+4, w-4)
	height := min(contentLines+2, h-2)
	inner, rows = width-4, height-2
	x, y = (w-width)/2, 1+(h-2-height)/2
	for row := range height {
		u.screen.PutStrStyled(x, y+row, fit("", width), uiBaseStyle)
	}
	u.screen.PutStrStyled(x, y, "┌"+strings.Repeat("─", width-2)+"┐", uiAccentStyle)
	u.screen.PutStrStyled(x, y+height-1, "└"+strings.Repeat("─", width-2)+"┘", uiAccentStyle)
	for row := 1; row < height-1; row++ {
		u.screen.Put(x, y+row, "│", uiAccentStyle)
		u.screen.Put(x+width-1, y+row, "│", uiAccentStyle)
	}
	label := " " + title + " "
	u.screen.PutStrStyled(x+2, y, fit(label, min(displayWidth(label), width-4)), uiAccentStyle)
	return x + 2, y + 1, inner, rows, true
}

// drawModal dims everything between the bars and centers a titled box of
// lines over it, starting at line *offset, which it clamps so the last page
// stays full. Used by overlays that take every key while open.
func (u *ui) drawModal(w, h int, title string, lines []modalLine, offset *int) {
	widest := 0
	for _, line := range lines {
		widest = max(widest, displayWidth(line.text))
	}
	x, y, inner, rows, ok := u.modalFrame(w, h, title, widest, len(lines))
	if !ok {
		return
	}
	*offset = max(0, min(*offset, len(lines)-rows))
	for row, line := range lines[*offset:min(len(lines), *offset+rows)] {
		u.screen.PutStrStyled(x, y+row, fit(line.text, inner), line.style)
	}
}

// dimBody dims every cell between the bars, leaving the chrome bright behind
// an overlay.
func (u *ui) dimBody(w, h int) {
	for y := 1; y < h-1; y++ {
		for x := range w {
			if text, style, cellWidth := u.screen.Get(x, y); cellWidth > 0 {
				u.screen.Put(x, y, text, style.Dim(true))
			}
		}
	}
}

// drawScrollbar draws a track of height cells at x, y with a thumb sized and
// placed for the visible lines starting at offset out of total. It draws
// nothing when everything fits, so a bar shows only when there's more to scroll to.
func (u *ui) drawScrollbar(x, y, height, total, offset, visible int) {
	if height < 1 || total <= visible {
		return
	}
	thumb := max(1, height*visible/total)
	pos := 0
	if span := total - visible; span > 0 {
		pos = min(height-thumb, offset*(height-thumb)/span)
	}
	for i := range height {
		if i >= pos && i < pos+thumb {
			u.screen.PutStrStyled(x, y+i, "█", uiAccentStyle)
		} else {
			u.screen.PutStrStyled(x, y+i, "░", uiMutedStyle)
		}
	}
}
