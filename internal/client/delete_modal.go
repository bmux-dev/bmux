package client

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Keep the picker and its cached terminal visible behind confirmation. Chrome
// remains outside the dimmed backdrop, and no key/input goes to the source app.
func (u *ui) drawDeleteModal(w, h int) {
	u.screen.HideCursor()
	if w <= 0 || h <= 2 {
		return
	}
	for y := 1; y < h-1; y++ {
		for x := 0; x < w; x++ {
			text, style, cellWidth := u.screen.Get(x, y)
			if cellWidth == 0 {
				continue
			}
			u.screen.Put(x, y, text, style.Dim(true))
		}
	}
	question := fmt.Sprintf("Delete %q?", u.deleteTarget.Name)
	counts := u.deleteModalCounts()
	preferredWidth := min(56, max(36, max(displayWidth(question), displayWidth(counts))+4))
	width := min(preferredWidth, w)
	if w >= 8 {
		width = min(preferredWidth, w-4)
	}
	inner := max(0, width-4)
	if inner == 0 {
		return
	}
	type line struct {
		text  string
		style tcell.Style
	}
	var lines []line
	add := func(text string, style tcell.Style) {
		for _, text := range pickerWrap(text, inner) {
			lines = append(lines, line{text, style})
		}
	}
	add(question, uiBaseStyle.Bold(true))
	add(counts, uiMutedStyle)
	add("", uiBaseStyle)
	if u.promptError != "" {
		add("Error: "+u.promptError, uiBaseStyle.Bold(true))
	}
	controls := []line{{"Yes (y)   No (n / Esc)", uiAccentStyle}}
	height := min(h-2, len(lines)+len(controls)+2)
	x, y := (w-width)/2, 1+(h-2-height)/2
	for row := 0; row < height; row++ {
		u.screen.PutStrStyled(x, y+row, fit("", width), uiBaseStyle)
	}
	if height < 3 {
		return
	}
	u.screen.PutStrStyled(x, y, "┌"+strings.Repeat("─", width-2)+"┐", uiAccentStyle)
	u.screen.PutStrStyled(x, y+height-1, "└"+strings.Repeat("─", width-2)+"┘", uiAccentStyle)
	for row := 1; row < height-1; row++ {
		u.screen.Put(x, y+row, "│", uiAccentStyle)
		u.screen.Put(x+width-1, y+row, "│", uiAccentStyle)
	}
	title := " Delete session "
	u.screen.PutStrStyled(x+2, y, fit(title, min(displayWidth(title), width-4)), uiAccentStyle)
	// Reserve the last body rows for actions on short screens; never let a
	// lengthy session name or error push the safe confirmation keys offscreen.
	count := min(len(controls), height-2)
	bodyHeight := height - 2 - count
	for row := 0; row < min(len(lines), bodyHeight); row++ {
		u.screen.PutStrStyled(x+2, y+1+row, fit(lines[row].text, inner), lines[row].style)
	}
	for row := 0; row < count; row++ {
		u.screen.PutStrStyled(x+2, y+height-1-count+row, fit(controls[row].text, inner), controls[row].style)
	}
}

func (u *ui) deleteModalCounts() string {
	s := u.deleteTarget
	// Refresh presentation only for the captured instance. A replacement with
	// the same name must never substitute its counts or the action's identity.
	for _, current := range u.sessions {
		if current.Name == s.Name && current.Created.Equal(s.Created) {
			s = current
			break
		}
	}
	clients := s.Clients
	if u.conn != nil && u.name == s.Name && u.workspace != nil && u.workspace.Generation == protocol.SessionGeneration(s.Created) {
		clients = max(1, clients) // Our acknowledged attachment may precede a list refresh.
	}
	paneWord := "panes"
	if s.Panes == 1 {
		paneWord = "pane"
	}
	text := fmt.Sprintf("%d %s", s.Panes, paneWord)
	if clients > 0 {
		clientWord := "clients"
		if clients == 1 {
			clientWord = "client"
		}
		text += fmt.Sprintf(" · %d attached %s", clients, clientWord)
	}
	return text
}
