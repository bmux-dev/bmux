package client

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// The welcome screen opens over the first attached session while the config
// file says onboarding = true (the default), once per client. Continuing
// leads into the integrations panel; either way bmux writes
// onboarding = false so it isn't shown again.

// noteFirstAttach opens the welcome screen on the first frame of the first
// attachment, so the session is already visible behind it.
func (u *ui) noteFirstAttach() {
	if u.settings.Onboarding && !u.welcomed {
		u.welcomed, u.welcome = true, true
	}
}

func (u *ui) welcomeKey(e *tcell.EventKey) {
	switch {
	case e.Key() == tcell.KeyEnter || e.Key() == tcell.KeyRight || e.Key() == tcell.KeyRune && e.Rune() == 'l':
		u.finishWelcome()
		u.openIntegrations()
	case e.Key() == tcell.KeyEscape || e.Key() == tcell.KeyRune && e.Rune() == 'q':
		u.finishWelcome()
	}
}

func (u *ui) finishWelcome() {
	u.welcome = false
	u.settings.Onboarding = false
	if u.configPath == "" {
		return
	}
	if err := config.MarkOnboarded(u.configPath); err != nil {
		u.notice("Config: " + err.Error())
	}
}

func (u *ui) welcomeLines() []modalLine {
	root, _ := u.keyTables()
	row := func(keys, text string) modalLine {
		return modalLine{fmt.Sprintf("  %-15s %s", keys, text), uiBaseStyle}
	}
	lines := []modalLine{
		{"Sessions keep running when you detach, and come back with their", uiBaseStyle},
		{"layout, directories and agents after restarts and reboots.", uiBaseStyle},
		{"", uiBaseStyle},
	}
	help := "then a key runs a command"
	if k := u.prefixKey("help"); k != "" {
		help += "; " + k + " lists them"
	}
	lines = append(lines, row(u.prefixName(), help))
	if nav := directionKeys(root, "navigate"); nav != "" {
		lines = append(lines, row(nav, "move between panes, and into the sidebar on the left"))
	}
	if k := u.prefixKey("sessions"); k != "" {
		lines = append(lines, row(k, "session picker"))
	}
	if k := u.prefixKey("detach"); k != "" {
		lines = append(lines, row(k, "detach; the session keeps running"))
	}
	return append(lines,
		modalLine{"", uiBaseStyle},
		modalLine{"Next: install optional agent integrations for more reliable state.", uiAccentStyle},
		modalLine{"", uiBaseStyle},
		modalLine{"Enter, → or l continues · Esc skips", uiMutedStyle},
	)
}

func (u *ui) drawWelcome(w, h int) {
	offset := 0
	u.drawModal(w, h, "Welcome to bmux", u.welcomeLines(), &offset)
}
