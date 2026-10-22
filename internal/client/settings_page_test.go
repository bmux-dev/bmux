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
	boxTop := func() (int, int) {
		for y := range 24 {
			for x := range 110 {
				if text, _, _ := u.screen.Get(x, y); text == "┌" && x > 20 {
					return x, y
				}
			}
		}
		return -1, -1
	}
	listX, listY := boxTop()
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
	if text := screenText(u); !strings.Contains(text, "catppuccin-frappe") || !strings.Contains(text, "esc cancel") ||
		!strings.Contains(text, "Settings") || !strings.Contains(text, "Appearance") || !strings.Contains(text, "Sessions") {
		t.Fatalf("the choice list isn't a page inside Settings:\n%s", text)
	}
	if x, y := boxTop(); x != listX || y != listY {
		t.Fatalf("the choice page's box moved from %d,%d to %d,%d", listX, listY, x, y)
	}
	u.key(key(tcell.KeyRight))
	if u.settingsPage.tab != 1 {
		t.Fatalf("→ on the choice page switched to tab %d", u.settingsPage.tab)
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

func TestSettingsPageScrollbarShowsOnlyWhenThereIsMoreToScrollTo(t *testing.T) {
	u, _ := settingsPageUI(t, "")
	// A 24-row screen fits Sidebar's options but not the 25 themes.
	u.key(runeKey('l'))
	u.key(runeKey('l'))
	u.draw()
	text := screenText(u)
	if strings.ContainsAny(text, "█░") {
		t.Fatalf("a list that fits drew a scrollbar:\n%s", text)
	}
	u.key(runeKey('h'))
	u.key(key(tcell.KeyEnter)) // Theme choices
	column := func() (cells []string) {
		for y := range 24 {
			for x := range 110 {
				if text, _, _ := u.screen.Get(x, y); text == "█" || text == "░" {
					cells = append(cells, text)
					break
				}
			}
		}
		return cells
	}
	u.draw()
	top := column()
	if len(top) < 3 || top[0] != "█" || top[len(top)-1] != "░" {
		t.Fatalf("at the top the thumb should lead the track: %v", top)
	}
	u.key(runeKey('G'))
	u.draw()
	if bottom := column(); len(bottom) != len(top) || bottom[0] != "░" || bottom[len(bottom)-1] != "█" {
		t.Fatalf("at the end the thumb should close the track: %v", bottom)
	}
}

func TestSettingsTabsShowIconsWithSpaceBetween(t *testing.T) {
	u, _ := settingsPageUI(t, "")
	u.settings.UI.Icons = "plain"
	u.draw()
	if text := screenText(u); !strings.Contains(text, " ⌘ General   ◐ Appearance   ≡ Sidebar   ♪ Sound ") {
		t.Fatalf("tabs lack icons or spacing:\n%s", text)
	}
	// On a terminal too narrow for every tab, the current one still shows.
	u.screen.SetSize(60, 24)
	for range len(config.SettingTabs) - 1 {
		u.key(key(tcell.KeyRight))
	}
	u.draw()
	if text := screenText(u); !strings.Contains(text, "↻ Sessions") {
		t.Fatalf("the current tab scrolled out of a narrow bar:\n%s", text)
	}
}

func TestSettingsTabIconsFollowTheIconsSetting(t *testing.T) {
	const nerd, plain = "\uf013 General", "⌘ General"
	for _, tt := range []struct {
		name, icons string
		ascii       bool
		want, not   string
	}{
		{"auto uses nerd icons with powerline separators", "auto", false, nerd, plain},
		{"auto falls back when separators are ASCII", "auto", true, plain, nerd},
		{"nerd wins over ASCII separators", "nerd", true, nerd, plain},
		{"plain", "plain", false, plain, nerd},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := settingsPageUI(t, "")
			u.settings.UI.Icons, u.settings.UI.PowerlineASCII = tt.icons, tt.ascii
			u.draw()
			text := screenText(u)
			if !strings.Contains(text, tt.want) || strings.Contains(text, tt.not) {
				t.Fatalf("want %q and not %q:\n%s", tt.want, tt.not, text)
			}
		})
	}
}

// selectSetting moves the cursor to the setting with this key on the Appearance tab.
func selectSetting(t *testing.T, u *ui, key_ string) {
	t.Helper()
	u.key(runeKey('l'))
	for i, s := range config.SettingTabs[1].Settings {
		if s.Key == key_ {
			for range i {
				u.key(key(tcell.KeyDown))
			}
			return
		}
	}
	t.Fatalf("no setting %s", key_)
}

func TestSettingsPageOnlyClearsTheOverlayForChoicesItPreviews(t *testing.T) {
	for _, tt := range []struct {
		key     string
		clears  bool
		reasons string
	}{
		{"theme", true, "colors preview"},
		{"ui.icons", true, "icons preview"},
		{"ui.notice-position", false, "nothing to preview"},
		{"ui.graphics", false, "nothing to preview"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			u, _ := settingsPageUI(t, "")
			selectSetting(t, u, tt.key)
			u.key(key(tcell.KeyEnter))
			u.draw()
			if got := !dimmed(u, 105, 5); got != tt.clears {
				t.Fatalf("overlay cleared = %v, want %v (%s)", got, tt.clears, tt.reasons)
			}
		})
	}
}

func TestSettingsPageIconsPreviewWhileScrollingAndEscapeReverts(t *testing.T) {
	u, path := settingsPageUI(t, "")
	selectSetting(t, u, "ui.icons")
	u.key(key(tcell.KeyEnter)) // auto, so Nerd Font icons
	u.draw()
	if text := screenText(u); !strings.Contains(text, "\uf013 General") {
		t.Fatalf("auto should start on Nerd Font icons:\n%s", text)
	}
	u.key(key(tcell.KeyDown))
	u.key(key(tcell.KeyDown)) // plain
	u.draw()
	if text := screenText(u); !strings.Contains(text, "⌘ General") || strings.Contains(text, "\uf013") {
		t.Fatalf("scrolling to plain didn't preview the tab icons:\n%s", text)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("previewing icons wrote the file")
	}
	u.key(key(tcell.KeyEscape))
	u.draw()
	if text := screenText(u); !strings.Contains(text, "\uf013 General") {
		t.Fatalf("Esc didn't put the icons back:\n%s", text)
	}
	u.key(key(tcell.KeyEnter))
	u.key(key(tcell.KeyDown))
	u.key(key(tcell.KeyDown))
	u.key(key(tcell.KeyEnter))
	u.draw()
	if text := screenText(u); !strings.Contains(text, "⌘ General") || !strings.Contains(fileText(t, path), `icons = "plain"`) {
		t.Fatalf("Enter didn't keep plain icons:\n%s", text)
	}
}
