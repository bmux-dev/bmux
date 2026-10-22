package client

import (
	"strings"
	"time"
)

// Notices (a copy result, a deleted session, a hint) show in a small box with
// an accent border, placed by ui.notice-position over the pane area.
const (
	noticeMaxInner = 56 // widest text in the box, in columns
	noticeMaxLines = 4
)

// noticeText wraps text to at most noticeMaxLines lines of inner columns,
// ending the last with … when the text is longer.
func noticeText(text string, inner int) []string {
	lines := pickerWrap(strings.TrimSpace(text), inner)
	if len(lines) > noticeMaxLines {
		lines = lines[:noticeMaxLines]
		lines[noticeMaxLines-1] = strings.TrimRight(fit(lines[noticeMaxLines-1], inner-1), " ") + "…"
	}
	return lines
}

// noticeBox returns the top-left corner and size of a box of the given size
// for position within the area x0..w and rows top..bottom, or ok=false when
// it doesn't fit.
func noticeBox(position string, x0, w, top, bottom, width, height int) (x, y int, ok bool) {
	if width+2 > w-x0 || height > bottom-top {
		return 0, 0, false
	}
	switch {
	case strings.HasSuffix(position, "left"):
		x = x0 + 1
	case strings.HasSuffix(position, "center"):
		x = x0 + (w-x0-width)/2
	default:
		x = w - width - 1
	}
	if strings.HasPrefix(position, "bottom") {
		return x, bottom - height, true
	}
	return x, top, true
}

// noticeSample is the notice shown while choosing where notices appear.
const noticeSample = "Notices appear here"

func (u *ui) drawNotice(w, h int) {
	text, position := u.message, u.settings.UI.NoticePosition
	if preview, ok := u.noticePreview(); ok {
		text, position = noticeSample, preview // stays for as long as the choices are open
	} else if !time.Now().Before(u.messageUntil) {
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	x0 := 0
	if !u.picker && !u.projects.open {
		x0 = u.paneOriginX(w)
	}
	inner := min(noticeMaxInner, w-x0-6)
	if inner < 8 {
		return
	}
	lines := noticeText(text, inner)
	widest := 0
	for _, line := range lines {
		widest = max(widest, displayWidth(line))
	}
	width, height := widest+4, len(lines)+2
	// Rows between the powerline bar and the status bar.
	x, y, ok := noticeBox(position, x0, w, paneOriginY, h-1, width, height)
	if !ok {
		return
	}
	for row := range height {
		u.screen.PutStrStyled(x, y+row, fit("", width), uiBaseStyle)
	}
	u.screen.PutStrStyled(x, y, "┌"+strings.Repeat("─", width-2)+"┐", uiAccentStyle)
	u.screen.PutStrStyled(x, y+height-1, "└"+strings.Repeat("─", width-2)+"┘", uiAccentStyle)
	for row := 1; row < height-1; row++ {
		u.screen.Put(x, y+row, "│", uiAccentStyle)
		u.screen.Put(x+width-1, y+row, "│", uiAccentStyle)
		u.screen.PutStrStyled(x+2, y+row, fit(lines[row-1], width-4), uiBaseStyle)
	}
}
