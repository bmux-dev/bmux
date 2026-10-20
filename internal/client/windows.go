package client

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

// windows lists the attached session's windows from its latest frame.
func (u *ui) windows() []protocol.Window {
	if u.workspace == nil {
		return nil
	}
	return u.workspace.Windows
}

// activeWindow is the active window's 1-based number, or 0 before a frame.
func (u *ui) activeWindow() int {
	for i, w := range u.windows() {
		if w.Active {
			return i + 1
		}
	}
	return 0
}

// windowLabel is what the status bar calls a window: its name, else its
// focused pane's title, else "shell".
func windowLabel(w protocol.Window) string {
	label := w.Name
	if label == "" {
		label = strings.TrimSpace(w.Title)
	}
	if label == "" {
		label = "shell"
	}
	return label
}

// beginCloseWindow closes the active window (Ctrl-a &). Like closing a pane,
// it asks first if any of its panes has a program in front of the shell, or
// if bmux can't tell.
func (u *ui) beginCloseWindow() {
	number := u.activeWindow()
	if number == 0 || u.conn == nil {
		return
	}
	r, err := Request(u.socket, protocol.Request{Op: "pane-command", Name: u.name, Kind: "window", Window: number})
	if err == nil && r.Command == "" {
		u.send(protocol.Request{Op: "close-window", Window: number})
		return
	}
	u.closeWindow, u.closeCommand = number, r.Command
	if err != nil || u.closeCommand == "" {
		u.closeCommand = "a program"
	}
}

// beginRenameWindow opens a status-line prompt holding the window's name.
func (u *ui) beginRenameWindow() {
	number := u.activeWindow()
	if number == 0 {
		return
	}
	u.renameWindow, u.prompt, u.promptError = number, "rename-window", ""
	u.input = u.windows()[number-1].Name
}

// renameWindowKey edits the rename prompt. Enter sends the name (empty goes
// back to the pane's title); Esc keeps the old one.
func (u *ui) renameWindowKey(e *tcell.EventKey) {
	switch e.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		u.prompt, u.promptError = "", ""
	case tcell.KeyEnter:
		u.send(protocol.Request{Op: "rename-window", Window: u.renameWindow, NewName: strings.TrimSpace(u.input)})
		u.prompt, u.promptError = "", ""
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlH:
		u.input = dropRune(u.input)
	case tcell.KeyCtrlU:
		u.input = ""
	case tcell.KeyRune:
		u.appendWindowName(string(e.Rune()))
	}
}

// appendWindowName adds typed or pasted text, dropping control characters
// and stopping at the server's limit.
func (u *ui) appendWindowName(text string) {
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if len(u.input)+utf8.RuneLen(r) > maxWindowName {
			u.promptError = fmt.Sprintf("at most %d bytes", maxWindowName)
			return
		}
		u.input += string(r)
	}
}

// maxWindowName matches the server's limit on window names.
const maxWindowName = 40

// windowTab is a window's label on the top bar, for clicks.
type windowTab struct{ start, end, number int }

// drawWindowTabs lists the windows on the top bar from column x up to limit:
// the active one in the accent, windows with a pane waiting on you marked.
func (u *ui) drawWindowTabs(x, limit int) {
	u.windowTabs = u.windowTabs[:0]
	waiting := u.waitingPanes()
	for i, w := range u.windows() {
		marker := ""
		for _, id := range w.Panes {
			if waiting[id] {
				marker = "!"
				break
			}
		}
		label := fmt.Sprintf(" %d %s%s ", i+1, strings.TrimSpace(fit(windowLabel(w), 16)), marker)
		width := displayWidth(label)
		if x+width > limit {
			if x < limit {
				u.screen.PutStrStyled(x, 0, fit(" …", limit-x), barStyle)
			}
			return
		}
		style := barStyle
		switch {
		case w.Active:
			style = tcell.StyleDefault.Foreground(chrome.background).Background(accentColor).Bold(true)
		case marker != "":
			style = barStyle.Bold(true)
		}
		u.screen.PutStrStyled(x, 0, label, style)
		u.windowTabs = append(u.windowTabs, windowTab{x, x + width, i + 1})
		x += width
	}
}

// waitingPanes are the attached session's panes that need you, from the
// sidebar's session list: attention or a blocked agent.
func (u *ui) waitingPanes() map[int]bool {
	result := make(map[int]bool)
	for _, s := range u.sessions {
		if s.Name != u.name {
			continue
		}
		for _, a := range s.Activity {
			if a.Attention || a.State == protocol.AgentBlocked {
				result[a.Pane] = true
			}
		}
	}
	return result
}

// windowClick selects the window whose tab is at column x, reporting whether
// there was one.
func (u *ui) windowClick(x int) bool {
	for _, tab := range u.windowTabs {
		if x >= tab.start && x < tab.end {
			if tab.number != u.activeWindow() {
				u.send(protocol.Request{Op: "select-window", Window: tab.number})
			}
			return true
		}
	}
	return false
}
