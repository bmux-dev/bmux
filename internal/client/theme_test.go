package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
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
	got := themeFor("catppuccin-latte", config.UI{Accent: "cyan"})
	want := chromeTheme{hex(0x4c4f69), hex(0xeff1f5), hex(0x7c7f93), hex(0xe6e9ef), hex(0x1e66f5), hex(0xbcc0cc), hex(0x4c4f69),
		hex(0xdf8e1d), hex(0xd20f39), hex(0x179299), hex(0x40a02b), hex(0x9ca0b0), true}
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
	base := themeFor("tokyonight-storm", config.UI{Accent: "cyan"})
	if base.accent != hex(0x7aa2f7) {
		t.Fatalf("cyan keeps the theme's accent: %v", base.accent)
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
	// Named accents pick the shade that suits the theme's lightness.
	if dark, light := themeFor("nord", config.UI{Accent: "amber"}), themeFor("gruvbox-light", config.UI{Accent: "amber"}); dark.accent != hex(0xd4b483) || light.accent != hex(0x855500) {
		t.Fatalf("amber: dark theme %v, light theme %v", dark.accent, light.accent)
	}
}

func TestDefaultThemesAreUnchanged(t *testing.T) {
	d := themeFor("dark", config.UI{Accent: "cyan"})
	if d.background != hex(0x0b0b0b) || d.accent != hex(0x83c8dc) || d.bar != hex(0x202020) || d.light {
		t.Fatalf("dark: %+v", d)
	}
	l := themeFor("light", config.UI{Accent: "cyan"})
	if l.background != hex(0xf5f5f5) || l.accent != hex(0x00657c) || l.bar != hex(0xe4e4e4) || !l.light {
		t.Fatalf("light: %+v", l)
	}
}
