package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

func settingsPageUI(t *testing.T, content string) (*ui, string) {
	t.Helper()
	u, _ := sidebarUI(t)
	u.screen.SetSize(110, 24)
	u.applySettings(config.Default())
	t.Cleanup(func() { d := config.Default(); applyTheme(themeFor(d.Theme, d.UI)) })
	u.configPath = filepath.Join(t.TempDir(), "config.toml")
	if content != "" {
		if err := os.WriteFile(u.configPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prefixed(u, runeKey(','))
	if !u.settingsPage.open {
		t.Fatal("Ctrl-a , didn't open the settings page")
	}
	return u, u.configPath
}

func typeText(u *ui, text string) {
	for _, r := range text {
		u.key(runeKey(r))
	}
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSettingsPageShowsTabsAndSavesChoices(t *testing.T) {
	u, path := settingsPageUI(t, "# my notes\n")
	u.draw()
	text := screenText(u)
	for _, want := range []string{"Settings", "General", "Appearance", "Sidebar", "Sessions", "Theme", "dark", "Check for updates", "on"} {
		if !strings.Contains(text, want) {
			t.Fatalf("page doesn't show %q:\n%s", want, text)
		}
	}
	// Enter on Theme cycles dark -> light, saves, and applies at once.
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if got := fileText(t, path); !strings.Contains(got, "# my notes\n") || !strings.Contains(got, `theme = "light"`) {
		t.Fatalf("file after Enter:\n%s", got)
	}
	if u.settings.Theme != "light" {
		t.Fatalf("client still on theme %q", u.settings.Theme)
	}
	u.draw()
	if text := screenText(u); !strings.Contains(text, "light") || !strings.Contains(text, "Saved theme") {
		t.Fatalf("page after saving:\n%s", text)
	}
	// Space toggles a bool: Check for updates is two rows down.
	u.key(runeKey('j'))
	u.key(runeKey('j'))
	u.key(runeKey(' '))
	if got := fileText(t, path); !strings.Contains(got, "[update]\ncheck = false") {
		t.Fatalf("file after toggling:\n%s", got)
	}
	if u.settings.Update.Check {
		t.Fatal("client still checks for updates")
	}
}

func TestSettingsPageEditsTypedValuesAndRejectsBadOnes(t *testing.T) {
	u, path := settingsPageUI(t, "")
	u.key(runeKey('l'))
	u.key(runeKey('l')) // Sidebar
	u.key(runeKey('j')) // Width
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !u.settingsPage.editing || u.settingsPage.input != "24" {
		t.Fatalf("editor: editing=%v input=%q", u.settingsPage.editing, u.settingsPage.input)
	}
	u.key(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	u.key(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	typeText(u, "99")
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	u.draw()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("an invalid width was written:\n%s", fileText(t, path))
	}
	if !u.settingsPage.editing || !strings.Contains(screenText(u), "must be between 12 and 60") {
		t.Fatalf("the refusal isn't shown with the editor still open:\n%s", screenText(u))
	}
	u.key(tcell.NewEventKey(tcell.KeyCtrlU, 0, tcell.ModCtrl))
	typeText(u, "30")
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if u.settingsPage.editing || !strings.Contains(fileText(t, path), "[sidebar]\nwidth = 30") {
		t.Fatalf("width not saved:\n%s", fileText(t, path))
	}
	if u.sidebarCols() != 30 {
		t.Fatalf("sidebar is %d columns wide", u.sidebarCols())
	}
}

func TestSettingsPageEscapeCancelsAnEditThenCloses(t *testing.T) {
	u, path := settingsPageUI(t, "")
	u.key(runeKey('l'))
	u.key(runeKey('l'))
	u.key(runeKey('j'))
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	typeText(u, "5")
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.settingsPage.editing || !u.settingsPage.open {
		t.Fatalf("Esc while editing: editing=%v open=%v", u.settingsPage.editing, u.settingsPage.open)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a cancelled edit was written")
	}
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.settingsPage.open {
		t.Fatal("Esc didn't close the page")
	}
}

func TestSettingsPageTakesKeysAndPasteInsteadOfPanes(t *testing.T) {
	u, requests := sidebarUI(t)
	u.applySettings(config.Default())
	u.configPath = filepath.Join(t.TempDir(), "config.toml")
	u.openSettings()
	u.key(runeKey('x'))
	u.handleRawPaste(&rawPasteEvent{Text: "pasted"})
	noRequest(t, requests)
	u.key(runeKey('l'))
	u.key(runeKey('j'))
	u.key(runeKey('j'))
	u.key(runeKey('j'))
	u.key(runeKey('j'))
	u.key(runeKey('j'))                                        // past Images
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)) // Appearance: working color
	u.handleRawPaste(&rawPasteEvent{Text: "#12ab\n34"})
	if u.settingsPage.input != "#12ab34" {
		t.Fatalf("pasted into the editor as %q", u.settingsPage.input)
	}
	noRequest(t, requests)
}
