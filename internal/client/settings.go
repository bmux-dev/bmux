package client

import (
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

// applySettings installs client settings from the config file: prefix,
// theme and state colors, sidebar visibility and sound. Sidebar width and
// visibility changes resize panes like Ctrl-a b does.
func (u *ui) applySettings(settings config.Config) {
	first := u.settings.Prefix == ""
	previous, wasHidden := u.settings.Sidebar, u.sidebarHidden
	u.settings = settings
	u.rootKeys, u.prefixKeys = settings.Bindings()
	applyTheme(themeFor(settings.Theme, settings.UI))
	if first || previous.Show != settings.Sidebar.Show {
		u.sidebarHidden = !settings.Sidebar.Show
	}
	if !first && u.screen != nil && (previous != settings.Sidebar || wasHidden != u.sidebarHidden) {
		u.sendResize()
	}
}

// reloadSettings rereads the config file. Problems are shown and invalid
// settings keep their defaults, so a typo never leaves you without keys.
func (u *ui) reloadSettings() {
	settings, err := config.Load(u.configPath, os.Getenv)
	u.applySettings(settings)
	if err != nil {
		u.notice("Config: " + err.Error())
	} else {
		u.notice("Config reloaded from " + u.configPath)
	}
}

// requestReload reloads this client and asks the server to reload its own
// settings; other attached clients follow when the server's config revision
// changes.
func (u *ui) requestReload() {
	u.reloadSettings()
	if u.socket == "" {
		return
	}
	if _, err := Request(u.socket, protocol.Request{Op: "reload-config"}); err != nil {
		u.notice("Config: server didn't reload: " + err.Error())
	}
}

// noteConfig reloads when the server reports a newer config revision, e.g.
// after `bmux reload`. The first frame of an attachment only records it.
func (u *ui) noteConfig(f *protocol.Frame) {
	known := u.configKnown
	u.configKnown = true
	if !known || f.Config == u.configEdge {
		u.configEdge = f.Config
		return
	}
	u.configEdge = f.Config
	u.reloadSettings()
}

func (u *ui) prefixLetter() rune {
	if letter, err := config.PrefixKey(u.settings.Prefix); err == nil {
		return letter
	}
	return 'a'
}

// isPrefix matches the configured Ctrl-letter, whichever way tcell reports it.
func (u *ui) isPrefix(e *tcell.EventKey) bool {
	return keyName(e) == "C-"+string(u.prefixLetter())
}

// prefixName is the prefix as the status line shows it, e.g. "Ctrl-a".
func (u *ui) prefixName() string { return "Ctrl-" + string(u.prefixLetter()) }
