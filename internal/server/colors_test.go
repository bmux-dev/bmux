package server

import (
	"image/color"
	"testing"

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
