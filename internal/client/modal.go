package client

import (
	"strings"

	"github.com/gdamore/tcell/v2"
)

type modalLine struct {
	text  string
	style tcell.Style
}

// drawModal dims everything between the bars and centers a titled box of
// lines over it, starting at line *offset, which it clamps so the last page
// stays full. Used by overlays that take every key while open.
func (u *ui) drawModal(w, h int, title string, lines []modalLine, offset *int) {
	u.screen.HideCursor()
	if w < 10 || h < 6 {
		return
	}
	for y := 1; y < h-1; y++ {
		for x := range w {
			if text, style, cellWidth := u.screen.Get(x, y); cellWidth > 0 {
				u.screen.Put(x, y, text, style.Dim(true))
			}
		}
	}
	widest := displayWidth(title) + 2
	for _, line := range lines {
		widest = max(widest, displayWidth(line.text))
	}
	width := min(widest+4, w-4)
	height := min(len(lines)+2, h-2)
	inner, rows := width-4, height-2
	*offset = max(0, min(*offset, len(lines)-rows))
	x, y := (w-width)/2, 1+(h-2-height)/2
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
	for row, line := range lines[*offset:min(len(lines), *offset+rows)] {
		u.screen.PutStrStyled(x+2, y+1+row, fit(line.text, inner), line.style)
	}
}
