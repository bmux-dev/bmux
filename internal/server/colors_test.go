package server

import (
	"image/color"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/vt"
)

func TestCellColorsKeepPaletteIndexes(t *testing.T) {
	p := &pane{term: vt.NewEmulator(20, 4), visible: true}
	p.term.SetIndexedColor(5, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	// Basic red, bright green, 256-color orange, truecolor, an app-redefined
	// palette entry, default colors, then a basic background.
	_, _ = p.term.WriteString("\x1b[31mA\x1b[92mB\x1b[38;5;208mC\x1b[38;2;1;2;3mD\x1b[35mE\x1b[0mF\x1b[41mG")

	want := []struct{ fg, bg string }{
		{"1", ""}, {"10", ""}, {"208", ""}, {"#010203", ""}, {"#123456", ""}, {"", ""}, {"", "1"},
	}
	f, _ := p.frame(0)
	for i, w := range want {
		if c := f.Rows[0][i]; c.FG != w.fg || c.BG != w.bg {
			t.Errorf("cell %d (%q) = fg %q bg %q, want fg %q bg %q", i, c.Text, c.FG, c.BG, w.fg, w.bg)
		}
	}
	preview := p.preview(20, 4)
	for i, w := range want {
		if c := preview.Rows[0][i]; c.FG != w.fg || c.BG != w.bg {
			t.Errorf("preview cell %d (%q) = fg %q bg %q, want fg %q bg %q", i, c.Text, c.FG, c.BG, w.fg, w.bg)
		}
	}
}

func TestOSC11BackgroundFillsDefaultCells(t *testing.T) {
	p := &pane{term: vt.NewEmulator(6, 2), visible: true}
	p.trackBackground()
	// Default cell, explicit red background, then an untouched blank cell.
	_, _ = p.term.WriteString("\x1b]11;#0a0a0a\x07A\x1b[41mB\x1b[0m")

	f, _ := p.frame(0)
	if got := f.Rows[0][0].BG; got != "#0a0a0a" {
		t.Errorf("default cell bg = %q, want #0a0a0a", got)
	}
	if got := f.Rows[0][1].BG; got != "1" {
		t.Errorf("explicit bg = %q, want palette 1", got)
	}
	if got := f.Rows[1][5].BG; got != "#0a0a0a" {
		t.Errorf("blank cell bg = %q, want #0a0a0a", got)
	}
	if got := p.preview(6, 2).Rows[1][5].BG; got != "#0a0a0a" {
		t.Errorf("preview blank cell bg = %q, want #0a0a0a", got)
	}

	// OSC 111 hands the background back to the attached terminal.
	_, _ = p.term.WriteString("\x1b]111\x07")
	if f, _ = p.frame(0); f.Rows[0][0].BG != "" {
		t.Errorf("after reset bg = %q, want terminal default", f.Rows[0][0].BG)
	}
}

// reply is what a program reads after writing query to the pane.
func reply(t *testing.T, p *pane, query string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := p.term.Read(buf)
		done <- string(buf[:n])
	}()
	_, _ = p.term.WriteString(query)
	select {
	case got := <-done:
		return got
	case <-time.After(2 * time.Second):
		t.Fatalf("no reply to %q", query)
		return ""
	}
}

func useDefaults(t *testing.T, cfg config.Config, terminal *protocol.TerminalColors) {
	t.Helper()
	old, oldTerminal := activeSettings.Load(), terminalColors.Load()
	t.Cleanup(func() { activeSettings.Store(old); terminalColors.Store(oldTerminal) })
	activeSettings.Store(settingsFrom(cfg))
	terminalColors.Store(terminal)
}

// Programs that ask for the terminal's colors (omp, Neovim) learn what they
// are drawn on, so they pick a matching theme of their own.
func TestPanesAnswerColorQueriesWithTheirDefaults(t *testing.T) {
	themed := config.Default()
	themed.Theme = "catppuccin-latte"
	terminalMode := themed
	terminalMode.UI.PaneBackground = "terminal"
	themesOff := themed
	themesOff.UI.ThemePanes = false
	reported := &protocol.TerminalColors{Foreground: "#aabbcc", Background: "#101010"}

	for _, tc := range []struct {
		name           string
		cfg            config.Config
		terminal       *protocol.TerminalColors
		wantBG, wantFG string
	}{
		{"theme paints panes", themed, reported, "efef/f1f1/f5f5", "4c4c/4f4f/6969"},
		{"theme text on the terminal's background", terminalMode, reported, "1010/1010/1010", "4c4c/4f4f/6969"},
		{"terminal background not measured", terminalMode, nil, "efef/f1f1/f5f5", "4c4c/4f4f/6969"},
		{"theme panes off reports the terminal", themesOff, reported, "1010/1010/1010", "aaaa/bbbb/cccc"},
		{"dark leaves the emulator's own", config.Default(), nil, "0000/0000/0000", "ffff/ffff/ffff"},
	} {
		useDefaults(t, tc.cfg, tc.terminal)
		p := &pane{term: vt.NewEmulator(10, 2), visible: true}
		p.applyDefaultColors()
		if got, want := reply(t, p, "\x1b]11;?\x07"), "\x1b]11;rgb:"+tc.wantBG+"\x07"; got != want {
			t.Errorf("%s: background reply %q, want %q", tc.name, got, want)
		}
		if got, want := reply(t, p, "\x1b]10;?\x07"), "\x1b]10;rgb:"+tc.wantFG+"\x07"; got != want {
			t.Errorf("%s: foreground reply %q, want %q", tc.name, got, want)
		}
	}
}

func TestReloadAndAttachUpdateRunningPanes(t *testing.T) {
	useDefaults(t, config.Default(), nil)
	p := &pane{term: vt.NewEmulator(10, 2), visible: true}
	srv := &Server{sessions: map[string]*session{"s": {panes: map[int]*pane{1: p}}}}

	mocha := config.Default()
	mocha.Theme = "catppuccin-mocha"
	activeSettings.Store(settingsFrom(mocha))
	srv.applyPaneColors()
	if got := reply(t, p, "\x1b]11;?\x07"); got != "\x1b]11;rgb:1e1e/1e1e/2e2e\x07" {
		t.Fatalf("after a reload to a themed config: %q", got)
	}
	mocha.UI.ThemePanes = false
	activeSettings.Store(settingsFrom(mocha))
	terminalColors.Store(&protocol.TerminalColors{Background: "#000000"})
	srv.applyPaneColors()
	if got := reply(t, p, "\x1b]11;?\x07"); got != "\x1b]11;rgb:0000/0000/0000\x07" {
		t.Fatalf("after a terminal attached: %q", got)
	}
}
