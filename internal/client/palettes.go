package client

import (
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// palettes holds the chrome colors of every config.ThemeNames entry, from
// config.Palettes.
var palettes = func() map[string]chromeTheme {
	themes := make(map[string]chromeTheme, len(config.Palettes))
	for name, p := range config.Palettes {
		c := tcell.NewHexColor
		themes[name] = chromeTheme{c(p.Foreground), c(p.Background), c(p.Muted), c(p.Bar), c(p.Accent), c(p.Selection), c(p.SelectionText),
			c(p.Working), c(p.Blocked), c(p.Waiting), c(p.Idle), c(p.Unknown), p.Light, tcell.ColorDefault, tcell.ColorDefault}
	}
	return themes
}()
