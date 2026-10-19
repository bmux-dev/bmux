package client

import (
	"image/color"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// Chrome has its own palette; application cells keep their supplied colors.
// Agent state colors follow herdr's Catppuccin defaults (Mocha, or Latte for
// the light theme): working yellow, blocked red, done teal, idle green.
type chromeTheme struct {
	foreground, background, muted, bar, accent, selection, selectionText tcell.Color
	working, blocked, done, idle, unknown                                tcell.Color
}

func themeFor(name string, settings config.UI) chromeTheme {
	light := name == "light"
	t := chromeTheme{tcell.NewHexColor(0xe7e7e7), tcell.NewHexColor(0x0b0b0b), tcell.NewHexColor(0x929292), tcell.NewHexColor(0x202020), tcell.NewHexColor(0x83c8dc), tcell.NewHexColor(0x555555), tcell.NewHexColor(0xeeeeee),
		tcell.NewHexColor(0xf9e2af), tcell.NewHexColor(0xf38ba8), tcell.NewHexColor(0x94e2d5), tcell.NewHexColor(0xa6e3a1), tcell.NewHexColor(0x6c7086)}
	if light {
		t = chromeTheme{tcell.NewHexColor(0x202020), tcell.NewHexColor(0xf5f5f5), tcell.NewHexColor(0x555555), tcell.NewHexColor(0xe4e4e4), tcell.NewHexColor(0x00657c), tcell.NewHexColor(0xc7c7c7), tcell.NewHexColor(0x202020),
			tcell.NewHexColor(0xdf8e1d), tcell.NewHexColor(0xd20f39), tcell.NewHexColor(0x179299), tcell.NewHexColor(0x40a02b), tcell.NewHexColor(0x9ca0b0)}
	}
	switch settings.Accent {
	case "amber":
		t.accent = map[bool]tcell.Color{false: tcell.NewHexColor(0xd4b483), true: tcell.NewHexColor(0x855500)}[light]
	case "green":
		t.accent = map[bool]tcell.Color{false: tcell.NewHexColor(0xa4c987), true: tcell.NewHexColor(0x326320)}[light]
	case "", "cyan":
	default:
		if c := tcell.GetColor(settings.Accent); c.Valid() {
			t.accent = c
		}
	}
	for _, o := range []struct {
		value  string
		target *tcell.Color
	}{{settings.AgentColors.Working, &t.working}, {settings.AgentColors.Waiting, &t.done}, {settings.AgentColors.Blocked, &t.blocked}, {settings.AgentColors.Idle, &t.idle}} {
		if c := tcell.GetColor(o.value); o.value != "" && c.Valid() {
			*o.target = c
		}
	}
	return t
}

// Chrome styles are derived from the active theme by applyTheme, at startup
// and on config reload. Only the UI goroutine reads them.
var (
	chrome                                   chromeTheme
	accentColor                              tcell.Color
	barStyle, uiBaseStyle, uiMutedStyle      tcell.Style
	uiAccentStyle, dividerStyle              tcell.Style
	selectionBackground, selectionForeground tcell.Color
	sidebarBaseStyle, sidebarMutedStyle      tcell.Style
	sidebarAccentStyle, sidebarSelectedStyle tcell.Style
	sidebarBlockedStyle, sidebarDoneStyle    tcell.Style
	sidebarWorkingStyle, sidebarIdleStyle    tcell.Style
)

func init() { d := config.Default(); applyTheme(themeFor(d.Theme, d.UI)) }

func applyTheme(t chromeTheme) {
	chrome = t
	accentColor = t.accent
	barStyle = tcell.StyleDefault.Foreground(t.foreground).Background(t.bar)
	uiBaseStyle = tcell.StyleDefault.Foreground(t.foreground).Background(t.background)
	uiMutedStyle = uiBaseStyle.Foreground(t.muted)
	uiAccentStyle = uiBaseStyle.Foreground(t.accent).Bold(true)
	dividerStyle = tcell.StyleDefault.Foreground(t.muted)
	selectionBackground, selectionForeground = t.selection, t.selectionText
	// The sidebar uses the bar background so it reads as chrome, apart from panes.
	sidebarBaseStyle = barStyle
	sidebarMutedStyle = barStyle.Foreground(t.muted)
	sidebarAccentStyle = barStyle.Foreground(t.accent).Bold(true)
	sidebarBlockedStyle = barStyle.Foreground(t.blocked)
	sidebarDoneStyle = barStyle.Foreground(t.done)
	sidebarWorkingStyle = barStyle.Foreground(t.working)
	sidebarIdleStyle = barStyle.Foreground(t.idle)
	sidebarSelectedStyle = barStyle.Background(t.selection).Foreground(t.selectionText).Bold(true)
}

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
