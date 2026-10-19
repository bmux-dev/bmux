package client

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// helpLines lists the bindings in effect, in config.Actions order: keys
// that work without the prefix, then keys after it.
func (u *ui) helpLines() []string {
	root, prefix := u.keyTables()
	var lines []string
	section := func(title string, table map[string]string, prefixed bool) {
		lines = append(lines, title)
		for _, action := range config.Actions {
			keys := keysFor(table, action.Name)
			if len(keys) == 0 {
				continue
			}
			label := strings.Join(keys, " ")
			if prefixed {
				label = u.prefixName() + " " + strings.Join(keys, " / ")
			}
			lines = append(lines, "  "+label+strings.Repeat(" ", max(1, 18-displayWidth(label)))+action.Help)
		}
		lines = append(lines, "")
	}
	section("Without the prefix", root, false)
	section("After "+u.prefixName(), prefix, true)
	return append(lines, "Rebind them in [keys.root] and [keys.prefix] of the config file.")
}

// helpKey scrolls the key list; any other key closes it without reaching
// the application.
func (u *ui) helpKey(e *tcell.EventKey) {
	switch {
	case e.Key() == tcell.KeyDown || e.Key() == tcell.KeyRune && e.Rune() == 'j':
		u.helpOffset++
	case e.Key() == tcell.KeyUp || e.Key() == tcell.KeyRune && e.Rune() == 'k':
		u.helpOffset--
	case e.Key() == tcell.KeyPgDn || e.Key() == tcell.KeyRune && e.Rune() == ' ':
		u.helpOffset += 10
	case e.Key() == tcell.KeyPgUp:
		u.helpOffset -= 10
	default:
		u.help = false
	}
}

// drawHelp centers the key list over the dimmed panes, between the bars.
func (u *ui) drawHelp(w, h int) {
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
	lines := u.helpLines()
	widest := 0
	for _, line := range lines {
		widest = max(widest, displayWidth(line))
	}
	width := min(widest+4, w-4)
	height := min(len(lines)+2, h-2)
	inner, rows := width-4, height-2
	u.helpOffset = max(0, min(u.helpOffset, len(lines)-rows))
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
	u.screen.PutStrStyled(x+2, y, fit(" Keys ", min(6, width-4)), uiAccentStyle)
	for row, line := range lines[u.helpOffset:min(len(lines), u.helpOffset+rows)] {
		style := uiBaseStyle
		if !strings.HasPrefix(line, " ") && line != "" {
			style = uiAccentStyle
		}
		u.screen.PutStrStyled(x+2, y+1+row, fit(line, inner), style)
	}
}
