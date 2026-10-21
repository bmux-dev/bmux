package client

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// settingsPage is the overlay for changing the config file's settings: one
// tab per area, a list of options per tab. Each change goes straight to the
// file through config.Setting.Save and reloads, so the page never holds
// edits that aren't on disk.
type settingsPage struct {
	open     bool
	tab, row int
	offset   int           // first option shown, when a tab is taller than the box
	values   config.Config // the file's settings, without environment overrides
	editing  bool
	input    string
	message  string
	failed   bool // message is an error
}

func (u *ui) openSettings() {
	u.settingsPage = settingsPage{open: true}
	u.loadSettingsPage()
}

// loadSettingsPage rereads the file, so the page shows what is saved.
func (u *ui) loadSettingsPage() {
	u.settingsPage.values, _ = config.Load(u.configPath, nil)
}

func (u *ui) pageSetting() config.Setting {
	return config.SettingTabs[u.settingsPage.tab].Settings[u.settingsPage.row]
}

// settingsKey takes every key while the page is open; nothing reaches panes.
func (u *ui) settingsKey(e *tcell.EventKey) {
	p := &u.settingsPage
	if p.editing {
		switch {
		case e.Key() == tcell.KeyEnter:
			u.saveSetting(p.input)
		case e.Key() == tcell.KeyEscape:
			p.editing, p.message = false, ""
		case e.Key() == tcell.KeyBackspace || e.Key() == tcell.KeyBackspace2 || e.Key() == tcell.KeyCtrlH:
			p.input = dropRune(p.input)
		case e.Key() == tcell.KeyCtrlU:
			p.input = ""
		case e.Key() == tcell.KeyRune && e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0:
			p.input += string(e.Rune())
		}
		return
	}
	tabs := config.SettingTabs
	moveTab := func(delta int) {
		p.tab = (p.tab + delta + len(tabs)) % len(tabs)
		p.row, p.offset, p.message = 0, 0, ""
	}
	switch {
	case e.Key() == tcell.KeyEscape || e.Key() == tcell.KeyRune && e.Rune() == 'q':
		p.open = false
	case e.Key() == tcell.KeyRight || e.Key() == tcell.KeyTab || e.Key() == tcell.KeyRune && e.Rune() == 'l':
		moveTab(1)
	case e.Key() == tcell.KeyLeft || e.Key() == tcell.KeyBacktab || e.Key() == tcell.KeyRune && e.Rune() == 'h':
		moveTab(-1)
	case e.Key() == tcell.KeyDown || e.Key() == tcell.KeyRune && e.Rune() == 'j':
		p.row, p.message = min(p.row+1, len(tabs[p.tab].Settings)-1), ""
	case e.Key() == tcell.KeyUp || e.Key() == tcell.KeyRune && e.Rune() == 'k':
		p.row, p.message = max(p.row-1, 0), ""
	case e.Key() == tcell.KeyEnter || e.Key() == tcell.KeyRune && e.Rune() == ' ':
		u.changeSetting()
	case e.Key() == tcell.KeyRune && e.Rune() == 'e' && u.pageSetting().Custom:
		p.editing, p.input, p.message = true, u.pageSetting().Get(p.values), ""
	}
}

// changeSetting cycles a choice or opens the editor for a typed value.
func (u *ui) changeSetting() {
	p := &u.settingsPage
	s := u.pageSetting()
	current := s.Get(p.values)
	if s.Kind != config.KindBool && s.Kind != config.KindEnum {
		p.editing, p.input, p.message = true, current, ""
		return
	}
	next := s.Options[0] // also where a custom value goes next
	for i, option := range s.Options {
		if option == current {
			next = s.Options[(i+1)%len(s.Options)]
		}
	}
	u.saveSetting(next)
}

// saveSetting writes the selected setting, then reloads like Ctrl-a r so the
// change applies without leaving the page. A refused value stays in the
// editor with the reason.
func (u *ui) saveSetting(input string) {
	p := &u.settingsPage
	s := u.pageSetting()
	if err := s.Save(u.configPath, input); err != nil {
		p.message, p.failed = err.Error(), true
		return
	}
	p.editing, p.failed = false, false
	p.message = "Saved " + s.Key
	u.requestReload()
	u.loadSettingsPage()
}

// settingValue is how a value reads in the list.
func settingValue(s config.Setting, c config.Config) (string, bool) {
	value := s.Get(c)
	switch {
	case s.Kind == config.KindBool && value == "true":
		return "on", true
	case s.Kind == config.KindBool:
		return "off", true
	case value == "":
		return "(none)", false
	}
	return value, true
}

func (u *ui) drawSettings(w, h int) {
	p := &u.settingsPage
	tabs := config.SettingTabs
	tallest, labelWidth := 0, 0
	for _, tab := range tabs {
		tallest = max(tallest, len(tab.Settings))
		for _, s := range tab.Settings {
			labelWidth = max(labelWidth, displayWidth(s.Label))
		}
	}
	// Tab bar, rule, options, blank, help, message.
	x, y, inner, rows, ok := u.modalFrame(w, h, "Settings", 70, tallest+5)
	if !ok {
		return
	}
	listHeight := rows - 5
	if listHeight < 1 {
		return
	}
	u.drawSettingsTabs(x, y, inner)
	u.screen.PutStrStyled(x, y+1, strings.Repeat("─", inner), uiMutedStyle)

	options := tabs[p.tab].Settings
	if p.row < p.offset {
		p.offset = p.row
	} else if p.row >= p.offset+listHeight {
		p.offset = p.row - listHeight + 1
	}
	for i := p.offset; i < min(len(options), p.offset+listHeight); i++ {
		s := options[i]
		value, set := settingValue(s, p.values)
		selected := i == p.row
		if selected && p.editing {
			value, set = tailFit(p.input, max(1, inner-labelWidth-5))+"_", true
		}
		label, valueStyle := uiBaseStyle, uiMutedStyle
		marker := "  "
		if selected {
			marker, label, valueStyle = "▸ ", uiAccentStyle, uiAccentStyle
		} else if set {
			valueStyle = uiBaseStyle
		}
		row := y + 2 + i - p.offset
		u.screen.PutStrStyled(x, row, fit(marker+s.Label, labelWidth+3), label)
		u.screen.PutStrStyled(x+labelWidth+3, row, fit(value, max(0, inner-labelWidth-3)), valueStyle)
	}
	help := options[p.row].Help
	u.screen.PutStrStyled(x, y+rows-2, fit(help, inner), uiMutedStyle)
	messageStyle := uiMutedStyle
	if p.failed {
		messageStyle = uiBaseStyle.Foreground(tcell.ColorRed)
	}
	u.screen.PutStrStyled(x, y+rows-1, fit(p.message, inner), messageStyle)
}

// drawSettingsTabs draws the tab names, scrolled so the current one shows.
func (u *ui) drawSettingsTabs(x, y, width int) {
	tabs := config.SettingTabs
	label := func(i int) string { return " " + tabs[i].Name + " " }
	first, used := 0, 0
	for i := 0; i <= u.settingsPage.tab; i++ {
		used += displayWidth(label(i))
	}
	for first < u.settingsPage.tab && used > width {
		used -= displayWidth(label(first))
		first++
	}
	u.screen.PutStrStyled(x, y, fit("", width), uiBaseStyle)
	for i := first; i < len(tabs); i++ {
		style := uiMutedStyle
		if i == u.settingsPage.tab {
			style = uiAccentStyle.Reverse(true)
		}
		text := label(i)
		cells := displayWidth(text)
		if cells > width {
			text, cells = fit(text, width), width
		}
		u.screen.PutStrStyled(x, y, text, style)
		x, width = x+cells, width-cells
		if width <= 0 {
			break
		}
	}
}
