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
	if hint := u.updateHint(); hint != nil {
		lines = append(lines, updateHintMessage(hint), "")
	}
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
	var lines []modalLine
	for _, line := range u.helpLines() {
		style := uiBaseStyle
		if !strings.HasPrefix(line, " ") && line != "" {
			style = uiAccentStyle
		}
		lines = append(lines, modalLine{line, style})
	}
	u.drawModal(w, h, "Keys", lines, &u.helpOffset)
}
