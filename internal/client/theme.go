package client

import (
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
)

// Chrome has its own palette; application cells keep their supplied colors.
// Agent state colors follow herdr's Catppuccin defaults: working yellow,
// blocked red, done teal, idle green. Named themes use their scheme's own
// equivalents; see palettes.
type chromeTheme struct {
	foreground, background, muted, bar, accent, selection, selectionText tcell.Color
	working, blocked, done, idle, unknown                                tcell.Color
	light                                                                bool
	// paneForeground and paneBackground replace the terminal's default colors
	// in panes, dividers and blank space; ColorDefault leaves them alone.
	paneForeground, paneBackground tcell.Color
}

// themeFor builds the chrome palette for a theme name, then applies the ui
// accent and agent color overrides. An unrecognised name gets the default.
func themeFor(name string, settings config.UI) chromeTheme {
	name, _ = config.ResolveTheme(name)
	t, ok := palettes[name]
	if !ok {
		name = config.Default().Theme
		t = palettes[name]
	}
	t.paneForeground, t.paneBackground = tcell.ColorDefault, tcell.ColorDefault
	if p, foreground, background := (config.Config{Theme: name, UI: settings}).PaneColors(); foreground {
		t.paneForeground = tcell.NewHexColor(p.Foreground)
		if background {
			t.paneBackground = tcell.NewHexColor(p.Background)
		}
	}
	light := t.light
	switch settings.Accent {
	case "cyan":
		t.accent = map[bool]tcell.Color{false: tcell.NewHexColor(0x83c8dc), true: tcell.NewHexColor(0x00657c)}[light]
	case "amber":
		t.accent = map[bool]tcell.Color{false: tcell.NewHexColor(0xd4b483), true: tcell.NewHexColor(0x855500)}[light]
	case "green":
		t.accent = map[bool]tcell.Color{false: tcell.NewHexColor(0xa4c987), true: tcell.NewHexColor(0x326320)}[light]
	case "", "theme":
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
	paneStyle                                tcell.Style
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
	// Pane cells without a color of their own start from paneStyle: the
	// terminal's defaults, or the theme's when it paints the panes.
	paneStyle = tcell.StyleDefault.Foreground(t.paneForeground).Background(t.paneBackground)
	dividerStyle = paneStyle.Foreground(t.muted)
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
