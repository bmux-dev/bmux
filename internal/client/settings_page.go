package client

import (
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// settingsPage is the overlay for changing the config file's settings: one
// tab per area, a list of options per tab. Each change goes straight to the
// file through config.Setting.Save and reloads, so the page never holds
// edits that aren't on disk. The one exception is the look of a choice being
// scrolled through, which previews until Enter saves it or Esc puts it back.
type settingsPage struct {
	open     bool
	tab, row int
	offset   int           // first line shown, when a tab is taller than the box
	values   config.Config // the file's settings, without environment overrides
	editing  bool
	input    string
	message  string
	failed   bool // message is an error

	choosing     bool // the list of choices for the selected setting is open
	choice       int  // highlighted choice; len(Options) is the Custom row
	choiceOffset int  // first choice shown
}

// customChoice is the last row of a choice list that also takes a typed value.
const customChoice = "Custom…"

func (u *ui) openSettings() {
	u.settingsPage = settingsPage{open: true}
	u.loadSettingsPage()
}

// closeSettings closes the page, putting back any previewed look.
func (u *ui) closeSettings() {
	u.revertPreview()
	u.settingsPage.open = false
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
	if p.choosing {
		u.choiceKey(e)
		return
	}
	tabs := config.SettingTabs
	moveTab := func(delta int) {
		p.tab = (p.tab + delta + len(tabs)) % len(tabs)
		p.row, p.offset, p.message = 0, 0, ""
	}
	switch {
	case e.Key() == tcell.KeyEscape || e.Key() == tcell.KeyRune && e.Rune() == 'q':
		u.closeSettings()
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
	}
}

// changeSetting flips a switch, opens the list of choices, or opens the
// editor for a typed value.
func (u *ui) changeSetting() {
	p := &u.settingsPage
	s := u.pageSetting()
	switch s.Kind {
	case config.KindBool:
		current := s.Get(p.values)
		next := s.Options[0]
		for i, option := range s.Options {
			if option == current {
				next = s.Options[(i+1)%len(s.Options)]
			}
		}
		u.saveSetting(next)
	case config.KindEnum:
		u.openChoices()
	default:
		p.editing, p.input, p.message = true, s.Get(p.values), ""
	}
}

// openChoices opens the list of choices with the saved one highlighted.
func (u *ui) openChoices() {
	p := &u.settingsPage
	s := u.pageSetting()
	current := s.Get(p.values)
	if s.Key == "theme" {
		if theme, ok := config.ResolveTheme(current); ok {
			current = theme // "catppuccin" is catppuccin-mocha
		}
	}
	p.choosing, p.choice, p.choiceOffset, p.message, p.failed = true, 0, 0, "", false
	if s.Custom {
		p.choice = len(s.Options) // a value that isn't a preset is custom
	}
	for i, option := range s.Options {
		if option == current {
			p.choice = i
		}
	}
}

// choiceKey handles keys while a list of choices is open.
func (u *ui) choiceKey(e *tcell.EventKey) {
	p := &u.settingsPage
	s := u.pageSetting()
	last := len(s.Options) - 1
	if s.Custom {
		last++
	}
	move := func(to int) {
		p.choice, p.message = max(0, min(to, last)), ""
		u.previewChoice()
	}
	switch {
	case e.Key() == tcell.KeyEscape:
		u.revertPreview()
		p.message = ""
	case e.Key() == tcell.KeyDown || e.Key() == tcell.KeyRune && e.Rune() == 'j':
		move(p.choice + 1)
	case e.Key() == tcell.KeyUp || e.Key() == tcell.KeyRune && e.Rune() == 'k':
		move(p.choice - 1)
	case e.Key() == tcell.KeyPgDn:
		move(p.choice + 8)
	case e.Key() == tcell.KeyPgUp:
		move(p.choice - 8)
	case e.Key() == tcell.KeyHome || e.Key() == tcell.KeyRune && e.Rune() == 'g':
		move(0)
	case e.Key() == tcell.KeyEnd || e.Key() == tcell.KeyRune && e.Rune() == 'G':
		move(last)
	case e.Key() == tcell.KeyEnter && p.choice >= len(s.Options):
		u.revertPreview()
		p.editing, p.input, p.message = true, s.Get(p.values), ""
	case e.Key() == tcell.KeyEnter:
		u.saveSetting(s.Options[p.choice])
	}
}

// previewChoice shows the highlighted choice without saving it, for the
// settings that change colors. Anything else waits for Enter.
func (u *ui) previewChoice() {
	p := &u.settingsPage
	s := u.pageSetting()
	if p.choice >= len(s.Options) {
		u.applySaved() // Custom: nothing to show until a value is typed
		return
	}
	c := u.settings
	switch s.Key {
	case "theme":
		c.Theme = s.Options[p.choice]
	case "ui.accent":
		c.UI.Accent = s.Options[p.choice]
	case "ui.pane-background":
		c.UI.PaneBackground = s.Options[p.choice]
	default:
		return
	}
	applyTheme(themeFor(c.Theme, c.UI))
}

// applySaved shows the colors of the settings the client is running.
func (u *ui) applySaved() { applyTheme(themeFor(u.settings.Theme, u.settings.UI)) }

// revertPreview leaves the list of choices and puts back the applied theme.
func (u *ui) revertPreview() {
	if u.settingsPage.choosing {
		u.settingsPage.choosing = false
		u.applySaved()
	}
}

// saveSetting writes the selected setting, then reloads like Ctrl-a r so the
// change applies without leaving the page. A refused value stays in the
// editor or list of choices with the reason.
func (u *ui) saveSetting(input string) {
	p := &u.settingsPage
	s := u.pageSetting()
	if err := s.Save(u.configPath, input); err != nil {
		p.message, p.failed = err.Error(), true
		return
	}
	p.editing, p.choosing, p.failed = false, false, false
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

// settingsLine is one line of a tab: a group header, the gap before one, or
// the setting at index row.
type settingsLine struct {
	header string
	row    int // index into the tab's settings, or -1 for a header or gap
}

// settingsLines lays out a tab's settings under their group headers.
func settingsLines(settings []config.Setting) []settingsLine {
	var lines []settingsLine
	for i, s := range settings {
		if i == 0 || s.Group != settings[i-1].Group {
			if i > 0 {
				lines = append(lines, settingsLine{row: -1})
			}
			if s.Group != "" {
				lines = append(lines, settingsLine{header: s.Group, row: -1})
			}
		}
		lines = append(lines, settingsLine{row: i})
	}
	return lines
}

// drawSettingsFooter draws the rule and the line of keys at the box's bottom.
func (u *ui) drawSettingsFooter(x, y, inner, rows int, hint string) {
	u.screen.PutStrStyled(x, y+rows-2, strings.Repeat("─", inner), uiMutedStyle)
	u.screen.PutStrStyled(x, y+rows-1, fit(hint, inner), uiMutedStyle)
}

// drawSettings draws the page. A list of choices takes the place of the
// tab's options inside the same box, title and footer rows, with the tabs
// shown but not selectable. It leaves the screen behind undimmed, so a theme
// shows its true colors while you scroll.
func (u *ui) drawSettings(w, h int) {
	p := &u.settingsPage
	tabs := config.SettingTabs
	tallest, labelWidth := 0, 0
	for _, tab := range tabs {
		tallest = max(tallest, len(settingsLines(tab.Settings)))
		for _, s := range tab.Settings {
			labelWidth = max(labelWidth, displayWidth(s.Label))
		}
	}
	// Tab bar, rule, lines, blank, help, message, rule, keys.
	x, y, inner, rows, ok := u.modalBox(w, h, "Settings", max(70, u.tabsWidth()), tallest+7, !p.choosing)
	if !ok {
		return
	}
	listHeight := rows - 7
	if listHeight < 1 || p.choosing && listHeight < 2 {
		return
	}
	u.drawSettingsTabs(x, y, inner)
	u.screen.PutStrStyled(x, y+1, strings.Repeat("─", inner), uiMutedStyle)

	options := tabs[p.tab].Settings
	footer := "⏎/␣ change · ←/→ tabs · esc close"
	switch {
	case p.editing:
		footer = "⏎ save · esc cancel"
	case p.choosing:
		footer = "⏎ select · ↑/↓ scroll · esc cancel"
		u.drawChoices(x, y+2, inner, listHeight)
	}
	if !p.choosing {
		u.drawSettingsList(x, y+2, inner, listHeight, labelWidth)
	}
	u.screen.PutStrStyled(x, y+rows-4, fit(options[p.row].Help, inner), uiMutedStyle)
	messageStyle := uiMutedStyle
	if p.failed {
		messageStyle = uiBaseStyle.Foreground(tcell.ColorRed)
	}
	u.screen.PutStrStyled(x, y+rows-3, fit(p.message, inner), messageStyle)
	u.drawSettingsFooter(x, y, inner, rows, footer)
}

// drawSettingsList draws the tab's options under their group headers.
func (u *ui) drawSettingsList(x, y, inner, listHeight, labelWidth int) {
	p := &u.settingsPage
	options := config.SettingTabs[p.tab].Settings
	lines := settingsLines(options)
	selected := 0
	for i, line := range lines {
		if line.row == p.row {
			selected = i
		}
	}
	top := selected // keep a group's header with its first setting
	if top > 0 && lines[top-1].header != "" {
		top--
	}
	if top < p.offset {
		p.offset = top
	} else if selected >= p.offset+listHeight {
		p.offset = selected - listHeight + 1
	}
	for i := p.offset; i < min(len(lines), p.offset+listHeight); i++ {
		row := y + i - p.offset
		line := lines[i]
		if line.row < 0 {
			u.screen.PutStrStyled(x, row, fit(line.header, inner), uiBaseStyle.Bold(true))
			continue
		}
		s := options[line.row]
		value, set := settingValue(s, p.values)
		isSelected := line.row == p.row
		if isSelected && p.editing {
			value, set = tailFit(p.input, max(1, inner-labelWidth-6))+"_", true
		}
		label, valueStyle := uiBaseStyle, uiMutedStyle
		marker := "  "
		if isSelected {
			marker, label, valueStyle = "▸ ", uiAccentStyle, uiAccentStyle
		} else if set {
			valueStyle = uiBaseStyle
		}
		u.screen.PutStrStyled(x, row, fit("  "+marker+s.Label, labelWidth+5), label)
		u.screen.PutStrStyled(x+labelWidth+5, row, fit(value, max(0, inner-labelWidth-5)), valueStyle)
	}
	u.drawScrollbar(x+inner, y, listHeight, len(lines), p.offset, listHeight)
}

// drawChoices draws the selected setting's name and its choices, scrolled so
// the highlighted one shows.
func (u *ui) drawChoices(x, y, inner, listHeight int) {
	p := &u.settingsPage
	s := u.pageSetting()
	labels := slices.Clone(s.Options)
	if s.Custom {
		labels = append(labels, customChoice)
	}
	u.screen.PutStrStyled(x, y, fit(s.Label, inner), uiBaseStyle.Bold(true))
	height := listHeight - 1
	if p.choice < p.choiceOffset {
		p.choiceOffset = p.choice
	} else if p.choice >= p.choiceOffset+height {
		p.choiceOffset = p.choice - height + 1
	}
	current := s.Get(p.values)
	if s.Key == "theme" {
		if theme, ok := config.ResolveTheme(current); ok {
			current = theme
		}
	}
	for i := p.choiceOffset; i < min(len(labels), p.choiceOffset+height); i++ {
		text, style := "    "+labels[i], uiBaseStyle
		if i == p.choice {
			text, style = "  ▸ "+labels[i], uiAccentStyle
		}
		if labels[i] == current || i >= len(s.Options) && !slices.Contains(s.Options, current) {
			text += " ✓"
		}
		u.screen.PutStrStyled(x, y+1+i-p.choiceOffset, fit(text, inner), style)
	}
	u.drawScrollbar(x+inner, y+1, height, len(labels), p.choiceOffset, height)
}

// tabGap is the space between tab labels.
const tabGap = 1

// tabLabel is a tab's icon and name with a cell of padding either side. A
// terminal that can't draw the icon gets the name alone.
func (u *ui) tabLabel(i int) string {
	tab := config.SettingTabs[i]
	if tab.Icon != "" && u.screen.CanDisplay([]rune(tab.Icon)[0], false) {
		return " " + tab.Icon + " " + tab.Name + " "
	}
	return " " + tab.Name + " "
}

// tabsWidth is the width of the whole tab bar.
func (u *ui) tabsWidth() int {
	width := -tabGap
	for i := range config.SettingTabs {
		width += displayWidth(u.tabLabel(i)) + tabGap
	}
	return width
}

// drawSettingsTabs draws the tab names, scrolled so the current one shows.
func (u *ui) drawSettingsTabs(x, y, width int) {
	tabs := config.SettingTabs
	first, used := 0, -tabGap
	for i := 0; i <= u.settingsPage.tab; i++ {
		used += displayWidth(u.tabLabel(i)) + tabGap
	}
	for first < u.settingsPage.tab && used > width {
		used -= displayWidth(u.tabLabel(first)) + tabGap
		first++
	}
	u.screen.PutStrStyled(x, y, fit("", width), uiBaseStyle)
	for i := first; i < len(tabs); i++ {
		style := uiMutedStyle
		if i == u.settingsPage.tab {
			style = uiAccentStyle.Reverse(true)
		}
		text := u.tabLabel(i)
		cells := displayWidth(text)
		if cells > width {
			text, cells = fit(text, width), width
		}
		u.screen.PutStrStyled(x, y, text, style)
		x, width = x+cells+tabGap, width-cells-tabGap
		if width <= 0 {
			break
		}
	}
}
