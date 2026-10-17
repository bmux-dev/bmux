package client

import (
	"image/color"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Chrome has its own palette; application cells keep their supplied colors.
type chromeTheme struct{ foreground, background, muted, bar, accent, selection, selectionText tcell.Color }

func themeFromEnv(env func(string) string) chromeTheme {
	t := chromeTheme{tcell.NewHexColor(0xe7e7e7), tcell.NewHexColor(0x0b0b0b), tcell.NewHexColor(0x929292), tcell.NewHexColor(0x202020), tcell.NewHexColor(0x83c8dc), tcell.NewHexColor(0x555555), tcell.NewHexColor(0xeeeeee)}
	if strings.EqualFold(env("BMUX_THEME"), "light") {
		t = chromeTheme{tcell.NewHexColor(0x202020), tcell.NewHexColor(0xf5f5f5), tcell.NewHexColor(0x555555), tcell.NewHexColor(0xe4e4e4), tcell.NewHexColor(0x00657c), tcell.NewHexColor(0xc7c7c7), tcell.NewHexColor(0x202020)}
	}
	switch strings.ToLower(env("BMUX_ACCENT")) {
	case "amber":
		if strings.EqualFold(env("BMUX_THEME"), "light") {
			t.accent = tcell.NewHexColor(0x855500)
		} else {
			t.accent = tcell.NewHexColor(0xd4b483)
		}
	case "green":
		if strings.EqualFold(env("BMUX_THEME"), "light") {
			t.accent = tcell.NewHexColor(0x326320)
		} else {
			t.accent = tcell.NewHexColor(0xa4c987)
		}
	case "", "cyan":
	default:
		if value := env("BMUX_ACCENT"); strings.HasPrefix(value, "#") && len(value) == 7 {
			if c := tcell.GetColor(value); c.Valid() {
				t.accent = c
			}
		}
	}
	return t
}

var chrome = themeFromEnv(os.Getenv)
var accentColor = chrome.accent
var barStyle = tcell.StyleDefault.Foreground(chrome.foreground).Background(chrome.bar)
var uiBaseStyle = tcell.StyleDefault.Foreground(chrome.foreground).Background(chrome.background)
var uiMutedStyle = uiBaseStyle.Foreground(chrome.muted)
var uiAccentStyle = uiBaseStyle.Foreground(chrome.accent).Bold(true)
var selectionBackground = chrome.selection
var selectionForeground = chrome.selectionText

// The top powerline and bottom status row are chrome, never application cells.
const paneOriginY = 1

func paneViewportHeight(height int) int { return max(1, height-2) }

func previewDefaultColors() (color.RGBA, color.RGBA) {
	r, g, b := chrome.foreground.RGB()
	fg := color.RGBA{uint8(r), uint8(g), uint8(b), 255}
	background := color.RGBA{8, 8, 8, 255}
	if chrome.background == tcell.NewHexColor(0xf5f5f5) {
		background = color.RGBA{245, 245, 245, 255}
	}
	return background, fg
}
