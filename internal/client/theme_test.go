package client

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

func hex(v int32) tcell.Color { return tcell.NewHexColor(v) }

func TestEveryListedThemeHasItsOwnPalette(t *testing.T) {
	seen := map[chromeTheme]string{}
	for _, name := range config.ThemeNames {
		p, ok := palettes[name]
		if !ok {
			t.Errorf("%s has no palette", name)
			continue
		}
		if other, dup := seen[p]; dup {
			t.Errorf("%s and %s share a palette", name, other)
		}
		seen[p] = name
		if p.foreground == p.background || p.bar == p.background || p.selection == p.bar || p.selection == p.background {
			t.Errorf("%s: foreground, background, bar and selection must differ: %+v", name, p)
		}
	}
	if len(palettes) != len(config.ThemeNames) {
		t.Errorf("%d palettes for %d theme names", len(palettes), len(config.ThemeNames))
	}
}

func TestNamedThemeUsesItsSchemePalette(t *testing.T) {
	got := themeFor("catppuccin-latte", config.UI{Accent: "theme"})
	want := chromeTheme{hex(0x4c4f69), hex(0xeff1f5), hex(0x7c7f93), hex(0xe6e9ef), hex(0x1e66f5), hex(0xbcc0cc), hex(0x4c4f69),
		hex(0xdf8e1d), hex(0xd20f39), hex(0x179299), hex(0x40a02b), hex(0x9ca0b0), true, false}
	if got != want {
		t.Fatalf("catppuccin-latte:\n got %+v\nwant %+v", got, want)
	}
	if dracula := themeFor("dracula", config.UI{}); dracula.light || dracula.background != hex(0x282a36) || dracula.accent != hex(0xbd93f9) {
		t.Fatalf("dracula: %+v", dracula)
	}
}

func TestFamilyNameUsesTheDefaultVariant(t *testing.T) {
	for family, variant := range map[string]string{
		"catppuccin": "catppuccin-mocha", "tokyonight": "tokyonight-night", "gruvbox": "gruvbox-dark",
		"kanagawa": "kanagawa-wave", "everforest": "everforest-dark", "solarized": "solarized-dark",
	} {
		if themeFor(family, config.UI{}) != themeFor(variant, config.UI{}) {
			t.Errorf("%s doesn't resolve to %s", family, variant)
		}
	}
}

func TestUnknownThemeFallsBackToDark(t *testing.T) {
	if themeFor("neon", config.UI{}) != themeFor("dark", config.UI{}) {
		t.Fatal("unknown theme isn't dark")
	}
}

func TestUIOverridesApplyToANamedTheme(t *testing.T) {
	base := themeFor("tokyonight-storm", config.UI{Accent: "theme"})
	if base.accent != hex(0x7aa2f7) {
		t.Fatalf("the theme accent keeps the theme's own: %v", base.accent)
	}
	got := themeFor("tokyonight-storm", config.UI{
		Accent:      "#112233",
		AgentColors: config.AgentColors{Working: "#010203", Waiting: "#040506", Blocked: "#070809", Idle: "#0a0b0c"},
	})
	if got.accent != hex(0x112233) || got.working != hex(0x010203) || got.done != hex(0x040506) || got.blocked != hex(0x070809) || got.idle != hex(0x0a0b0c) {
		t.Fatalf("overrides not applied: %+v", got)
	}
	// Everything the overrides don't name stays the theme's.
	if got.foreground != base.foreground || got.background != base.background || got.bar != base.bar || got.selection != base.selection || got.unknown != base.unknown {
		t.Fatalf("overrides changed other colors: %+v", got)
	}
	// Named accents pick the shade that suits the theme's lightness; cyan is
	// its own color, not the theme's.
	if dark, light := themeFor("nord", config.UI{Accent: "cyan"}), themeFor("gruvbox-light", config.UI{Accent: "cyan"}); dark.accent != hex(0x83c8dc) || light.accent != hex(0x00657c) {
		t.Fatalf("cyan: dark theme %v, light theme %v", dark.accent, light.accent)
	}
	if dark, light := themeFor("nord", config.UI{Accent: "amber"}), themeFor("gruvbox-light", config.UI{Accent: "amber"}); dark.accent != hex(0xd4b483) || light.accent != hex(0x855500) {
		t.Fatalf("amber: dark theme %v, light theme %v", dark.accent, light.accent)
	}
}

func TestDefaultThemesAreUnchanged(t *testing.T) {
	d := themeFor("dark", config.UI{Accent: "theme"})
	if d.background != hex(0x0b0b0b) || d.accent != hex(0x83c8dc) || d.bar != hex(0x202020) || d.light {
		t.Fatalf("dark: %+v", d)
	}
	l := themeFor("light", config.UI{Accent: "theme"})
	if l.background != hex(0xf5f5f5) || l.accent != hex(0x00657c) || l.bar != hex(0xe4e4e4) || !l.light {
		t.Fatalf("light: %+v", l)
	}
}

func useTheme(t *testing.T, name string, ui config.UI) {
	t.Helper()
	applyTheme(themeFor(name, ui))
	t.Cleanup(func() { d := config.Default(); applyTheme(themeFor(d.Theme, d.UI)) })
}

func TestPanesFollowANamedThemeByDefault(t *testing.T) {
	if !config.Default().UI.ThemePanes {
		t.Fatal("ui.theme-panes isn't on by default")
	}
	useTheme(t, "catppuccin-mocha", config.Default().UI)
	fg, bg, _ := cellStyle(protocol.Cell{Text: "$"}).Decompose()
	if fg != hex(0xcdd6f4) || bg != hex(0x1e1e2e) {
		t.Fatalf("a plain cell is fg %v bg %v, want the theme's", fg, bg)
	}
	// Colors a program sets stay as set, whether palette, exact, or one of the two.
	fg, bg, _ = cellStyle(protocol.Cell{FG: "1", BG: "#102030"}).Decompose()
	if fg != tcell.PaletteColor(1) || bg != hex(0x102030) {
		t.Fatalf("explicit colors changed: fg %v bg %v", fg, bg)
	}
	fg, bg, _ = cellStyle(protocol.Cell{FG: "2"}).Decompose()
	if fg != tcell.PaletteColor(2) || bg != hex(0x1e1e2e) {
		t.Fatalf("a set foreground lost the theme background: fg %v bg %v", fg, bg)
	}
}

func TestPanesKeepTerminalColorsWhenThemePanesIsOffOrTheThemeIsPlain(t *testing.T) {
	off := config.Default().UI
	off.ThemePanes = false
	for name, c := range map[string]struct {
		theme string
		ui    config.UI
	}{
		"theme-panes off": {"catppuccin-mocha", off},
		"dark":            {"dark", config.Default().UI},
		"light":           {"light", config.Default().UI},
		"unknown name":    {"neon", config.Default().UI},
	} {
		useTheme(t, c.theme, c.ui)
		fg, bg, _ := cellStyle(protocol.Cell{Text: "$"}).Decompose()
		if fg != tcell.ColorDefault || bg != tcell.ColorDefault {
			t.Errorf("%s: a plain cell is fg %v bg %v, want the terminal's", name, fg, bg)
		}
	}
}

func TestThemedPanesPaintDividersAndKeepReverseVideo(t *testing.T) {
	useTheme(t, "catppuccin-mocha", config.Default().UI)
	s := focusTestScreen(t, 21, 9)
	u := &ui{screen: s}
	f := junctionTestFrame()
	u.applyFrame(f)
	for _, p := range f.Panes {
		u.drawPane(p, 21, 9)
	}
	u.drawPaneDividers(21, 9)
	for _, at := range [][2]int{{0, paneOriginY}, {10, paneOriginY + 1}} { // pane text, divider
		_, style, _ := s.Get(at[0], at[1])
		if _, bg, _ := style.Decompose(); bg != hex(0x1e1e2e) {
			t.Errorf("cell %v has background %v, want the theme's", at, bg)
		}
	}
	// Reverse video swaps the theme's colors rather than the terminal's.
	style := cellStyle(protocol.Cell{Text: "x", Attrs: uv.AttrReverse})
	if fg, bg, attrs := style.Decompose(); fg != hex(0xcdd6f4) || bg != hex(0x1e1e2e) || attrs&tcell.AttrReverse == 0 {
		t.Errorf("reverse cell: fg %v bg %v attrs %v", fg, bg, attrs)
	}
}
