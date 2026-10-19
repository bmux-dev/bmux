package client

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/integration"
)

// integrationRow is one agent in the integrations panel.
type integrationRow struct {
	agent  integration.Agent
	status integration.Status
	err    error // from reading its files, or from the last install
}

// integrationsPanel lists agent integrations like herdr's: ✓ installed,
// ↻ update available, + available (on PATH), – not found. Enter installs
// every available or outdated one; files are local, so it runs inline.
type integrationsPanel struct {
	open   bool
	rows   []integrationRow
	note   string
	offset int
	env    *integration.Env // nil: the current user's
}

func (u *ui) openIntegrations() {
	u.integrations.open, u.integrations.offset, u.integrations.note = true, 0, ""
	u.refreshIntegrations()
}

func (u *ui) integrationEnv() (integration.Env, error) {
	if u.integrations.env != nil {
		return *u.integrations.env, nil
	}
	return integration.System()
}

// refreshIntegrations rereads every status, keeping install errors.
func (u *ui) refreshIntegrations() {
	env, err := u.integrationEnv()
	if err != nil {
		u.integrations.rows, u.integrations.note = nil, err.Error()
		return
	}
	previous := u.integrations.rows
	u.integrations.rows = nil
	for i, a := range integration.All() {
		status, err := a.Status(env)
		if err == nil && i < len(previous) && previous[i].agent.Name == a.Name && status != integration.Installed {
			err = previous[i].err
		}
		u.integrations.rows = append(u.integrations.rows, integrationRow{a, status, err})
	}
}

func (u *ui) pendingIntegrations() int {
	n := 0
	for _, row := range u.integrations.rows {
		if row.status == integration.Available || row.status == integration.Outdated {
			n++
		}
	}
	return n
}

func (u *ui) installIntegrations() {
	env, err := u.integrationEnv()
	if err != nil {
		u.integrations.note = err.Error()
		return
	}
	var installed, failed []string
	var notes []string
	for i, row := range u.integrations.rows {
		if row.status != integration.Available && row.status != integration.Outdated {
			continue
		}
		if err := row.agent.Install(env); err != nil {
			u.integrations.rows[i].err = err
			failed = append(failed, row.agent.Label)
			continue
		}
		u.integrations.rows[i].err = nil
		installed = append(installed, row.agent.Label)
		if row.agent.Notes != "" {
			notes = append(notes, row.agent.Notes)
		}
	}
	u.refreshIntegrations()
	switch {
	case len(installed) == 0 && len(failed) == 0:
		u.integrations.note = "Nothing to install."
	case len(failed) > 0:
		u.integrations.note = "Couldn't install " + strings.Join(failed, ", ") + "; see the rows above."
	default:
		u.integrations.note = "Installed " + strings.Join(installed, ", ") + ". Restart running agents to load them."
	}
	if len(notes) > 0 {
		u.integrations.note += " " + strings.Join(notes, " ")
	}
}

// integrationsKey: Enter installs, Esc or q closes; nothing reaches panes.
func (u *ui) integrationsKey(e *tcell.EventKey) {
	switch {
	case e.Key() == tcell.KeyEnter:
		u.installIntegrations()
	case e.Key() == tcell.KeyEscape || e.Key() == tcell.KeyRune && e.Rune() == 'q':
		u.integrations.open = false
	case e.Key() == tcell.KeyDown || e.Key() == tcell.KeyRune && e.Rune() == 'j':
		u.integrations.offset++
	case e.Key() == tcell.KeyUp || e.Key() == tcell.KeyRune && e.Rune() == 'k':
		u.integrations.offset--
	}
}

func (u *ui) integrationLines() []modalLine {
	lines := []modalLine{
		{"Agents with an integration report their own state (working, blocked,", uiBaseStyle},
		{"idle) and exact session, instead of bmux guessing from their output.", uiBaseStyle},
		{"Installing changes each agent's own config; other settings are kept.", uiMutedStyle},
		{"", uiBaseStyle},
	}
	for _, row := range u.integrations.rows {
		mark, style := "–", uiMutedStyle
		switch row.status {
		case integration.Installed:
			mark, style = "✓", sidebarIdleStyle.Background(chrome.background)
		case integration.Outdated:
			mark, style = "↻", uiAccentStyle
		case integration.Available:
			mark, style = "+", uiAccentStyle
		}
		text := fmt.Sprintf("%s %-12s %s", mark, row.agent.Label, row.status)
		if row.err != nil {
			text += ": " + row.err.Error()
		}
		lines = append(lines, modalLine{text, style})
	}
	lines = append(lines, modalLine{"", uiBaseStyle})
	if u.integrations.note != "" {
		lines = append(lines, modalLine{u.integrations.note, uiBaseStyle}, modalLine{"", uiBaseStyle})
	}
	action := "Nothing to install"
	if n := u.pendingIntegrations(); n > 0 {
		action = fmt.Sprintf("Enter installs %d", n)
	}
	return append(lines, modalLine{action + " · Esc closes · bmux integration status for details", uiMutedStyle})
}

func (u *ui) drawIntegrations(w, h int) {
	u.drawModal(w, h, "Agent integrations", u.integrationLines(), &u.integrations.offset)
}
