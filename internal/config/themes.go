package config

import (
	"errors"
	"fmt"
	"strings"
)

// ThemeNames lists every theme `theme` accepts, in the order the settings page
// cycles them. The client holds a palette for each one.
var ThemeNames = []string{
	"dark", "light",
	"catppuccin-mocha", "catppuccin-macchiato", "catppuccin-frappe", "catppuccin-latte",
	"tokyonight-night", "tokyonight-storm", "tokyonight-moon", "tokyonight-day",
	"gruvbox-dark", "gruvbox-light",
	"nord",
	"rose-pine", "rose-pine-moon", "rose-pine-dawn",
	"kanagawa-wave", "kanagawa-dragon", "kanagawa-lotus",
	"onedark",
	"dracula",
	"everforest-dark", "everforest-light",
	"solarized-dark", "solarized-light",
}

// themeFamilies are scheme names without a variant; each picks the scheme's
// default.
var themeFamilies = map[string]string{
	"catppuccin": "catppuccin-mocha",
	"tokyonight": "tokyonight-night",
	"gruvbox":    "gruvbox-dark",
	"kanagawa":   "kanagawa-wave",
	"everforest": "everforest-dark",
	"solarized":  "solarized-dark",
}

// ResolveTheme returns the theme in ThemeNames that name selects, ignoring
// case and surrounding space. A family name such as "catppuccin" selects its
// default variant. ok is false for a name that matches nothing.
func ResolveTheme(name string) (theme string, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if family, found := themeFamilies[name]; found {
		return family, true
	}
	for _, known := range ThemeNames {
		if name == known {
			return known, true
		}
	}
	return "", false
}

// themeChoices describes what `theme` accepts, for the error shown when it
// doesn't.
const themeChoices = "dark, light, catppuccin, tokyonight, gruvbox, nord, rose-pine, kanagawa, onedark, dracula, everforest or solarized, " +
	"optionally with a variant such as catppuccin-latte"

// SaveTheme writes the theme name selects to the config file at path, keeping
// the rest of the file as it is, and returns the theme it saved. Unlike the
// file's own `theme`, an unknown name is an error rather than a fallback.
func SaveTheme(path, name string) (string, error) {
	theme, ok := ResolveTheme(name)
	if !ok {
		return "", fmt.Errorf("unknown theme %q; bmux theme --list shows the names", name)
	}
	for _, tab := range SettingTabs {
		for _, s := range tab.Settings {
			if s.Key == "theme" {
				return theme, s.Save(path, theme)
			}
		}
	}
	return "", errors.New("the theme setting is missing")
}

// PaneColors says what themed panes show where a program set no color of its
// own, which is all a theme ever changes there, as a terminal's does. It
// returns the theme's palette and whether its foreground and background apply.
// dark and light, ui.theme-panes = false, and ui.pane-background = "terminal"
// (for the background) leave the terminal's own colors in place.
func (c Config) PaneColors() (p Palette, foreground, background bool) {
	if !c.UI.ThemePanes || c.Theme == "dark" || c.Theme == "light" {
		return Palette{}, false, false
	}
	p, ok := Palettes[c.Theme]
	return p, ok, ok && c.UI.PaneBackground == "theme"
}
