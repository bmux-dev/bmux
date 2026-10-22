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
	for _, want := range []string{"Settings", "General", "Appearance", "Sidebar", "Sessions", "Keys", "Updates", "Prefix key", "Check for updates", "on",
		"⏎/␣ change · ←/→ tabs · esc close"} {
		if !strings.Contains(text, want) {
			t.Fatalf("page doesn't show %q:\n%s", want, text)
		}
	}
	// Theme leads the Appearance tab, under its own header. Enter opens the
	// choices; picking one saves it and applies it at once.
	u.key(runeKey('l'))
	u.draw()
	if text := screenText(u); !strings.Contains(text, "Theme") || !strings.Contains(text, "dark") {
		t.Fatalf("Appearance doesn't lead with Theme:\n%s", text)
	}
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	u.key(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if got := fileText(t, path); !strings.Contains(got, "# my notes\n") || !strings.Contains(got, `theme = "light"`) {
		t.Fatalf("file after Enter:\n%s", got)
	}
	if u.settings.Theme != "light" {
		t.Fatalf("client still on theme %q", u.settings.Theme)
	}
	if u.settingsPage.choosing {
		t.Fatal("Enter left the choices open instead of going back to the list")
	}
	u.draw()
	if text := screenText(u); u.settingsPage.values.Theme != "light" || !strings.Contains(text, "Saved theme") {
		t.Fatalf("page after saving:\n%s", text)
	}
	// Space toggles a bool: Check for updates is on General, below the prefix.
	u.key(runeKey('h'))
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
	for i, setting := range config.SettingTabs[1].Settings {
		if setting.Key == "ui.agent-colors.working" {
			for range i {
				u.key(runeKey('j'))
			}
		}
	}
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)) // Appearance: working color
	u.handleRawPaste(&rawPasteEvent{Text: "#12ab\n34"})
	if u.settingsPage.input != "#12ab34" {
		t.Fatalf("pasted into the editor as %q", u.settingsPage.input)
	}
	noRequest(t, requests)
}

func key(k tcell.Key) *tcell.EventKey { return tcell.NewEventKey(k, 0, tcell.ModNone) }

// dimmed says whether the cell at x, y is drawn dim.
func dimmed(u *ui, x, y int) bool {
	_, style, _ := u.screen.Get(x, y)
	_, _, attrs := style.Decompose()
	return attrs&tcell.AttrDim != 0
}

func TestSettingsPageGroupsOptionsUnderHeaders(t *testing.T) {
	u, _ := settingsPageUI(t, "")
	u.key(runeKey('l')) // Appearance
	u.screen.SetSize(110, 40)
	u.draw()
	text := screenText(u)
	last := -1
	for _, header := range []string{"Display", "Agent colors"} {
		at := strings.Index(text, header)
		if at < 0 || at < last {
			t.Fatalf("header %q missing or out of order:\n%s", header, text)
		}
		last = at
	}
	// Moving down through every option reaches the last group and keeps it on screen.
	for range len(config.SettingTabs[1].Settings) {
		u.key(key(tcell.KeyDown))
	}
	u.screen.SetSize(110, 14)
	u.draw()
	if text := screenText(u); !strings.Contains(text, "Idle color") {
		t.Fatalf("the last option scrolled out of a short box:\n%s", text)
	}
}

func TestSettingsPageThemePreviewsWhileScrollingAndEscapeReverts(t *testing.T) {
	u, path := settingsPageUI(t, "")
	u.key(runeKey('l')) // Appearance, Theme
	u.draw()
	saved := chrome
	if !dimmed(u, 105, 5) {
		t.Fatal("the settings list should dim the screen behind it")
	}
	u.key(key(tcell.KeyEnter))
	if !u.settingsPage.choosing {
		t.Fatal("Enter on a choice didn't open its list")
	}
	u.key(key(tcell.KeyDown))
	u.key(key(tcell.KeyDown)) // catppuccin-mocha
	if chrome == saved {
		t.Fatal("scrolling didn't preview the theme")
	}
	want := themeFor("catppuccin-mocha", u.settings.UI)
	if chrome != want {
		t.Fatalf("preview chrome %+v, want %+v", chrome, want)
	}
	u.draw()
	if dimmed(u, 105, 5) {
		t.Fatal("the choice list dimmed the screen, which muddies the previewed colors")
	}
	if text := screenText(u); !strings.Contains(text, "catppuccin-frappe") || !strings.Contains(text, "esc cancel") {
		t.Fatalf("choice list:\n%s", text)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("previewing wrote the file:\n%s", fileText(t, path))
	}
	if u.settings.Theme != config.Default().Theme {
		t.Fatalf("previewing changed the client's theme to %q", u.settings.Theme)
	}
	u.key(key(tcell.KeyEscape))
	if u.settingsPage.choosing || !u.settingsPage.open {
		t.Fatalf("Esc: choosing=%v open=%v", u.settingsPage.choosing, u.settingsPage.open)
	}
	if chrome != saved {
		t.Fatal("Esc didn't put the saved theme back")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a reverted preview was written")
	}
}

func TestSettingsPageChoiceHighlightsSavedValueAndKeepsPreviewOffAnyOtherSetting(t *testing.T) {
	u, _ := settingsPageUI(t, "theme = \"nord\"\n[ui]\nnotice-position = \"bottom-left\"\n")
	u.key(runeKey('l'))
	u.key(key(tcell.KeyEnter))
	if got := config.SettingTabs[1].Settings[0].Options[u.settingsPage.choice]; got != "nord" {
		t.Fatalf("choices open on %q, want the saved theme", got)
	}
	u.key(key(tcell.KeyEscape))
	// Notice position is a choice too, but it has no look to preview.
	for i, s := range config.SettingTabs[1].Settings {
		if s.Key == "ui.notice-position" {
			for range i {
				u.key(key(tcell.KeyDown))
			}
		}
	}
	before := chrome
	u.key(key(tcell.KeyEnter))
	if got := u.pageSetting().Options[u.settingsPage.choice]; got != "bottom-left" {
		t.Fatalf("notice choices open on %q", got)
	}
	u.key(key(tcell.KeyDown))
	if chrome != before {
		t.Fatal("scrolling notice positions changed the colors")
	}
}

func TestSettingsPageAccentCustomRowOpensTheEditor(t *testing.T) {
	u, path := settingsPageUI(t, "")
	u.key(runeKey('l'))
	for i, s := range config.SettingTabs[1].Settings {
		if s.Key == "ui.accent" {
			for range i {
				u.key(key(tcell.KeyDown))
			}
		}
	}
	saved := chrome
	u.key(key(tcell.KeyEnter))
	u.key(key(tcell.KeyDown))
	u.key(key(tcell.KeyDown)) // amber previews
	if chrome.accent == saved.accent {
		t.Fatal("scrolling accents didn't preview them")
	}
	for range 5 {
		u.key(key(tcell.KeyDown)) // clamps on the Custom row
	}
	u.draw()
	if text := screenText(u); !strings.Contains(text, "Custom…") {
		t.Fatalf("no Custom… row:\n%s", text)
	}
	if chrome != saved {
		t.Fatal("the Custom row should show the saved look")
	}
	u.key(key(tcell.KeyEnter))
	if !u.settingsPage.editing || u.settingsPage.choosing {
		t.Fatalf("Custom…: editing=%v choosing=%v", u.settingsPage.editing, u.settingsPage.choosing)
	}
	u.key(key(tcell.KeyCtrlU))
	typeText(u, "#112233")
	u.key(key(tcell.KeyEnter))
	if got := fileText(t, path); !strings.Contains(got, `accent = "#112233"`) {
		t.Fatalf("custom accent not saved:\n%s", got)
	}
	// The saved custom value reopens on the Custom row.
	u.key(key(tcell.KeyEnter))
	if u.settingsPage.choice != len(u.pageSetting().Options) {
		t.Fatalf("custom value highlights choice %d", u.settingsPage.choice)
	}
}

func TestSettingsPageClickRevertsAPreview(t *testing.T) {
	u, _ := settingsPageUI(t, "")
	u.key(runeKey('l'))
	saved := chrome
	u.key(key(tcell.KeyEnter))
	u.key(key(tcell.KeyDown))
	u.mouse(tcell.NewEventMouse(5, 5, tcell.Button1, tcell.ModNone))
	if u.settingsPage.open || chrome != saved {
		t.Fatalf("click: open=%v, saved look back=%v", u.settingsPage.open, chrome == saved)
	}
}
