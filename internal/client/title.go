package client

import (
	"strings"
	"unicode"
)

// maxTitleRunes bounds what an app's title adds to the outer window title.
const maxTitleRunes = 200

// outerTitle names the outer terminal's window, like tmux's set-titles: the
// session, then the focused pane's own title (OSC 0/2) when its app set one.
func (u *ui) outerTitle() string {
	if u.picker || u.name == "" {
		return "bmux"
	}
	title := u.name
	if u.frame != nil {
		if pane := cleanTitle(u.frame.Title); pane != "" && pane != u.name {
			title += " · " + pane
		}
	}
	return title
}

// updateTitle sends the window title only when it changes. tcell saves the
// outer title when the screen starts and restores it when bmux exits.
func (u *ui) updateTitle() {
	if !u.settings.UI.SetTitle {
		u.title = ""
		return
	}
	if title := u.outerTitle(); title != u.title {
		u.title = title
		u.screen.SetTitle(title)
	}
}

// cleanTitle drops control characters, which could end the OSC sequence
// early and reach the outer terminal as commands, and caps the length.
func cleanTitle(title string) string {
	var b strings.Builder
	n := 0
	for _, r := range title {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			continue
		}
		if n == maxTitleRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
