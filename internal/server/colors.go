package server

import (
	"image/color"
	"sync/atomic"

	"github.com/josephferrero/bmux/internal/protocol"
)

// terminalColors are the default colors the last attaching terminal reported.
var terminalColors atomic.Pointer[protocol.TerminalColors]

// rgbColor is a 0xRRGGBB value as a color.
func rgbColor(v int32) color.Color { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

// parseHexColor reads "#rrggbb"; nil for anything else.
func parseHexColor(s string) color.Color {
	if len(s) != 7 || s[0] != '#' {
		return nil
	}
	var n int32
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9':
			n = n<<4 | (c - '0')
		case c >= 'a' && c <= 'f':
			n = n<<4 | (c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n = n<<4 | (c - 'A' + 10)
		default:
			return nil
		}
	}
	return rgbColor(n)
}

// paneDefaultColors are the colors panes tell programs that ask for the
// terminal's foreground and background (OSC 10 and 11), as a terminal does:
// what a program draws on wherever it sets no color. A theme's colors where
// bmux paints them, otherwise what the attached terminal reported, otherwise
// nil, which leaves the emulator's own black and white.
func paneDefaultColors() (fg, bg color.Color) {
	settings, terminal := currentSettings(), terminalColors.Load()
	fg, bg = settings.paneFG, settings.paneBG
	if fg == nil && terminal != nil {
		fg = parseHexColor(terminal.Foreground)
	}
	if bg == nil && terminal != nil {
		bg = parseHexColor(terminal.Background)
	}
	if bg == nil {
		bg = settings.themeBG
	}
	return fg, bg
}

// applyDefaultColors gives the pane's emulator the current defaults. The caller
// holds p.mu unless the pane isn't running yet.
func (p *pane) applyDefaultColors() {
	fg, bg := paneDefaultColors()
	p.term.SetDefaultForegroundColor(fg)
	p.term.SetDefaultBackgroundColor(bg)
}

// applyPaneColors gives every pane the current defaults, after a reload or a
// new terminal attaches.
func (s *Server) applyPaneColors() {
	s.mu.Lock()
	items := make([]*session, 0, len(s.sessions))
	for _, item := range s.sessions {
		items = append(items, item)
	}
	s.mu.Unlock()
	for _, item := range items {
		item.mu.Lock()
		panes := make([]*pane, 0, len(item.panes))
		for _, p := range item.panes {
			panes = append(panes, p)
		}
		item.mu.Unlock()
		for _, p := range panes {
			p.mu.Lock()
			p.applyDefaultColors()
			p.mu.Unlock()
		}
	}
}
