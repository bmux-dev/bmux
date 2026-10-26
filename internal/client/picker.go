package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/rivo/uniseg"
)

type pickerArea struct{ x, y, w, h int }

func pickerAreas(w, h int) (pickerArea, pickerArea) {
	list := pickerArea{0, 1, max(0, w), max(0, h-2)}
	if w >= 30 && h >= 16 {
		top := h / 2
		list.h = max(0, top-list.y)
		return list, pickerArea{1, top + 1, w - 2, max(0, h-top-3)}
	}
	return list, pickerArea{}
}

func (u *ui) selectedSession() (protocol.Session, bool) {
	matches := u.filtered()
	if len(matches) == 0 {
		return protocol.Session{}, false
	}
	u.chosen = max(0, min(u.chosen, len(matches)-1))
	return matches[u.chosen], true
}

func (u *ui) movePicker(delta int) {
	u.chosen = max(0, min(u.chosen+delta, len(u.filtered())-1))
}

// pickerLines lays matches out as list lines and returns the first line
// visible in a window of height lines that keeps the chosen session in view.
// Without a search, sessions of one project sit under a header.
func (u *ui) pickerLines(matches []protocol.Session, height int) (lines []sessionLine, start int) {
	if u.query == "" {
		lines = groupLines(matches, resolveRepo)
	} else {
		for i, s := range matches {
			lines = append(lines, sessionLine{session: s, index: i})
		}
	}
	chosen := 0
	for i, l := range lines {
		if l.index == u.chosen {
			chosen = i
		}
	}
	return lines, max(0, chosen-height+1)
}

func (u *ui) listKey(e *tcell.EventKey) {
	if u.searching {
		switch e.Key() {
		case tcell.KeyEscape:
			u.searching = false
			u.query = ""
			u.chosen = 0
		case tcell.KeyEnter:
			u.searching = false
		case tcell.KeyUp:
			u.movePicker(-1)
		case tcell.KeyDown:
			u.movePicker(1)
		case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlH:
			u.query = dropRune(u.query)
			u.chosen = 0
		case tcell.KeyCtrlU:
			u.query = ""
			u.chosen = 0
		case tcell.KeyCtrlC:
			u.quitting = true
		case tcell.KeyRune:
			if e.Modifiers() == tcell.ModNone {
				u.query += string(e.Rune())
				u.chosen = 0
			}
		}
		return
	}
	s, selected := u.selectedSession()
	switch e.Key() {
	case tcell.KeyEscape:
		if u.query != "" {
			u.query = ""
			u.chosen = 0
			return
		}
		if u.conn == nil {
			u.quitting = true
		} else {
			u.picker = false
			u.stopPreview()
			u.previewEpoch++
			u.send(protocol.Request{Op: "focus", Focused: true})
		}
	case tcell.KeyCtrlC:
		u.quitting = true
	case tcell.KeyDown, tcell.KeyCtrlJ:
		u.movePicker(1)
	case tcell.KeyUp, tcell.KeyCtrlK:
		u.movePicker(-1)
	case tcell.KeyCtrlN:
		u.openProjects()
	case tcell.KeyCtrlR:
		if selected {
			u.newName, u.input, u.prompt = s.Name, s.Name, "rename"
			u.renameTarget = s
		}
	case tcell.KeyEnter:
		if selected {
			if err := u.switchTo(s.Name); err != nil {
				u.notice(err.Error())
			}
		}
	case tcell.KeyRune:
		if e.Modifiers() != tcell.ModNone {
			return
		}
		switch e.Rune() {
		case 'j':
			u.movePicker(1)
		case 'k':
			u.movePicker(-1)
		case 'n':
			u.openProjects()
		case 'd':
			if selected {
				u.beginDelete(s)
			}
		case 'r':
			if selected {
				u.newName, u.input, u.prompt = s.Name, s.Name, "rename"
				u.renameTarget = s
			}
		case '/':
			u.searching = true
		}
	}
}

func (u *ui) pickerMouse(e *tcell.EventMouse, x, y int) {
	if u.prompt != "" {
		return
	}
	w, h := u.screen.Size()
	list, _ := pickerAreas(w, h)
	if x < list.x || x >= list.x+list.w || y < list.y || y >= list.y+list.h {
		return
	}
	switch {
	case e.Buttons()&tcell.WheelDown != 0:
		u.movePicker(1)
	case e.Buttons()&tcell.WheelUp != 0:
		u.movePicker(-1)
	case e.Buttons()&tcell.Button1 != 0:
		lines, start := u.pickerLines(u.filtered(), list.h)
		if line := start + y - list.y; line < len(lines) && lines[line].index >= 0 {
			u.chosen = lines[line].index
		}
	}
	u.requestPreview()
}

func (u *ui) drawPicker(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	u.drawPowerline(w, time.Now())
	if u.prompt != "" && u.prompt != "delete" {
		u.drawPickerPrompt(w, h)
		return
	}
	list, preview := pickerAreas(w, h)
	matches := u.filtered()
	u.chosen = max(0, min(u.chosen, len(matches)-1))
	lines, start := u.pickerLines(matches, list.h)
	end := min(len(lines), start+list.h)
	position := 0
	if len(matches) > 0 {
		position = u.chosen + 1
	}
	heading := fmt.Sprintf("%d/%d", position, len(matches))
	if len(lines) > list.h {
		first, last := -1, -1 // the sessions the window shows
		for _, l := range lines[start:end] {
			if l.index >= 0 {
				if first < 0 {
					first = l.index
				}
				last = l.index
			}
		}
		heading += fmt.Sprintf(" · %d-%d", first+1, last+1)
	}
	if len(matches) == 0 && list.h > 0 {
		message := "No sessions. Press n to create one."
		if u.query != "" {
			message = "No matches. / to edit; Esc to clear."
		}
		u.screen.PutStr(list.x, list.y, fit(message, list.w))
	}
	for row := start; row < end; row++ {
		l := lines[row]
		if l.index < 0 {
			u.screen.PutStrStyled(list.x, list.y+row-start, fit("      "+l.header, list.w), uiMutedStyle.Bold(true)) // under the session names
			continue
		}
		s, i := l.session, l.index
		attachment := ""
		if s.Clients > 0 {
			attachment = " (attached)"
			if s.Clients > 1 {
				attachment = fmt.Sprintf(" (attached: %d clients)", s.Clients)
			}
		}
		if u.conn != nil && s.Name == u.name {
			attachment = fmt.Sprintf(" (attached: here + %d others)", max(0, s.Clients-1))
		}
		style := uiBaseStyle
		if i == u.chosen {
			style = uiBaseStyle.Background(selectionBackground).Foreground(selectionForeground).Bold(true).Reverse(true)
		}
		if list.w < 64 && s.Clients > 0 {
			attachment = fmt.Sprintf(" (attached: %dc)", s.Clients)
		}
		if s.Restored {
			attachment = " (restored)" // its programs ended; nobody has looked yet
		}
		prefix := fmt.Sprintf("(%d) + ", i)
		label := s.Name
		if l.nested {
			branch := "├─"
			if l.last {
				branch = "└─"
			}
			prefix = "  " + branch + " " + prefix
		}
		if u.settings.UI.ShowBranch && l.branch != "" {
			label += " [" + l.branch + "]"
		}
		suffix := fmt.Sprintf(": %d panes%s", s.Panes, attachment)
		if s.Windows > 1 {
			suffix = fmt.Sprintf(": %d windows, %d panes%s", s.Windows, s.Panes, attachment)
		}
		name := strings.TrimSpace(fit(label, max(0, list.w-displayWidth(prefix)-displayWidth(suffix))))
		line := prefix + name + suffix
		u.screen.PutStrStyled(list.x, list.y+row-start, fit(line, list.w), style)
	}
	if preview.w > 0 && preview.h > 0 {
		u.drawPickerTerminalBorder(preview, w, heading)
		u.drawSessionPreview(preview)
	} else if h > 1 {
		u.screen.PutStrStyled(max(0, w-displayWidth(heading)), 0, heading, barStyle)
	}
	if u.searching && list.w > 0 {
		u.screen.ShowCursor(min(w-1, 3+displayWidth(u.query)), 0)
	}
	if u.prompt == "delete" {
		u.drawDeleteModal(w, h)
	}
}

func (u *ui) drawPickerTerminalBorder(area pickerArea, w int, position string) {
	top, bottom := area.y-1, area.y+area.h
	for x := 0; x < w; x++ {
		u.screen.Put(x, top, "─", uiMutedStyle)
		u.screen.Put(x, bottom, "─", uiMutedStyle)
	}
	for y := top + 1; y < bottom; y++ {
		u.screen.Put(0, y, "│", uiMutedStyle)
		u.screen.Put(w-1, y, "│", uiMutedStyle)
	}
	for _, corner := range []struct {
		x, y int
		s    string
	}{{0, top, "┌"}, {w - 1, top, "┐"}, {0, bottom, "└"}, {w - 1, bottom, "┘"}} {
		u.screen.Put(corner.x, corner.y, corner.s, uiMutedStyle)
	}
	name := u.previewName
	if name == "" {
		name = "no session"
	}
	nameWidth := max(0, w-displayWidth(position)-7)
	label := " " + strings.TrimSpace(fit(name, nameWidth)) + " · " + position + " "
	u.screen.PutStrStyled(1, top, fit(label, min(displayWidth(label), max(0, w-2))), barStyle)
}

func (u *ui) drawSessionPreview(area pickerArea) {
	if u.preview != nil && len(u.preview.Panes) == 0 {
		u.screen.PutStrStyled(area.x, area.y, fit("Closed · session has no running panes", area.w), uiMutedStyle)
		return
	}
	if u.preview == nil || !previewFitsPanel(u.preview, area) {
		text := "Loading…"
		if u.preview != nil {
			text = "Waiting for panel size…"
		}
		if u.previewName == "" {
			text = "Select a session to preview"
		}
		if u.previewError != "" {
			text = "Unavailable · " + u.previewError
		}
		u.screen.PutStr(area.x, area.y, fit(text, area.w))
		return
	}
	u.drawTerminalView(area)
}

// pickerFooter is shared with the single-row footer drawn by ui.draw.
func (u *ui) pickerFooter(width int) string {
	if u.prompt == "delete" {
		return "y yes · n/Esc no"
	}
	if u.prompt != "" {
		if width < 45 {
			return "Enter confirm Ctrl-u clear Esc cancel"
		}
		return "Enter confirm · Ctrl-u clear · Esc cancel"
	}
	if u.searching {
		if width < 45 {
			return "Type ↑↓ ↵done Esc/Ctrl-u:clear"
		}
		if width < 65 {
			return "Type filter ↑↓ · Enter done Esc clear Ctrl-u clear"
		}
		return "Type filter · ↑↓ select · Enter finish · Esc clear · Ctrl-u clear"
	}
	if width < 45 {
		return "j/k ↵attach n:new d:kill r:ren / Esc"
	}
	if width < 65 {
		return "j/k Enter:attach n:new d:kill r:rename /:find Esc:back"
	}
	return "j/k ↑↓ · Enter attach · n new · d terminate · r rename · / search · Esc back"
}

func (u *ui) drawPickerPrompt(w, h int) {
	x := 0
	if w >= 3 {
		x = 1
	}
	width := max(0, w-2*x)
	y := 2
	if h < 7 {
		y = 1
	}
	put := func(text string, style tcell.Style) {
		for _, line := range pickerWrap(text, width) {
			if y < h-1 {
				u.screen.PutStrStyled(x, y, fit(line, width), style)
			}
			y++
		}
	}
	label := "Rename session " + u.newName + ":"
	put(label, uiAccentStyle)
	put(u.input, uiBaseStyle)
	inputLines := pickerWrap(u.input, width)
	if y-1 < h-1 && width > 0 && len(inputLines) > 0 {
		u.screen.ShowCursor(min(w-x-1, x+displayWidth(inputLines[len(inputLines)-1])), y-1)
	}
	if u.promptError != "" {
		put("Error: "+u.promptError, uiBaseStyle.Bold(true))
	}
}

// Wrap chrome on grapheme boundaries; application output is never rewritten.
func pickerWrap(text string, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	var line strings.Builder
	used := 0
	g := uniseg.NewGraphemes(text)
	for g.Next() {
		part := g.Str()
		if strings.ContainsAny(part, "\n\r\t") {
			part = " "
		}
		cells := displayWidth(part)
		if used+cells > width && used > 0 {
			lines = append(lines, line.String())
			line.Reset()
			used = 0
		}
		if cells <= width {
			line.WriteString(part)
			used += cells
		}
	}
	return append(lines, line.String())
}
