package client

import (
	"testing"

	"github.com/bmux-dev/bmux/internal/protocol"
	"github.com/gdamore/tcell/v2"
)

func TestCellStyleUsesTerminalPalette(t *testing.T) {
	for _, tc := range []struct {
		name string
		want tcell.Color
	}{
		{"0", tcell.PaletteColor(0)},
		{"1", tcell.PaletteColor(1)},
		{"15", tcell.PaletteColor(15)},
		{"208", tcell.PaletteColor(208)},
		{"255", tcell.PaletteColor(255)},
		{"#000000", tcell.NewRGBColor(0, 0, 0)},
		{"#800000", tcell.NewHexColor(0x800000)},
		{"#AbCdEf", tcell.NewRGBColor(0xab, 0xcd, 0xef)},
		{"#FFFFFF", tcell.NewRGBColor(0xff, 0xff, 0xff)},
		{"red", tcell.GetColor("red")},
		{"rebeccapurple", tcell.GetColor("rebeccapurple")},
		{"256", tcell.ColorDefault},
		{"999", tcell.ColorDefault},
		{"1000", tcell.ColorDefault},
		{"#zzzzzz", tcell.ColorDefault},
		{"#12345", tcell.ColorDefault},
		{"#1234567", tcell.ColorDefault},
		{"notacolor", tcell.ColorDefault},
	} {
		if got := cellColor(tc.name); got != tc.want {
			t.Errorf("cellColor(%q) = %v, want %v", tc.name, got, tc.want)
		}
		fg, _, _ := cellStyle(protocol.Cell{FG: tc.name}).Decompose()
		_, bg, _ := cellStyle(protocol.Cell{BG: tc.name}).Decompose()
		if fg != tc.want || bg != tc.want {
			t.Errorf("color %q = fg %v bg %v, want %v", tc.name, fg, bg, tc.want)
		}
	}
	if got := cellColor(""); got != tcell.ColorDefault {
		t.Errorf("cellColor(\"\") = %v, want ColorDefault", got)
	}
}

func TestCellColorZeroAllocs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		color string
	}{
		{"hex", "#a1b2c3"},
		{"hex_upper", "#DEADBEEF"[:7]},
		{"palette", "208"},
		{"palette_oob", "256"},
		{"named", "rebeccapurple"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allocs := testing.AllocsPerRun(100, func() {
				_ = cellColor(tc.color)
			})
			if allocs != 0 {
				t.Fatalf("cellColor(%q) allocated %.1f times/op, want 0", tc.color, allocs)
			}
		})
	}
	cell := protocol.Cell{Text: "x", Width: 1, FG: "#12ab34", BG: "235"}
	allocs := testing.AllocsPerRun(100, func() {
		_ = cellStyle(cell)
	})
	if allocs != 0 {
		t.Fatalf("cellStyle allocated %.1f times/op, want 0", allocs)
	}
}

func BenchmarkCellColorHex(b *testing.B) {
	b.ReportAllocs()
	var c tcell.Color
	for i := 0; i < b.N; i++ {
		c = cellColor("#a1b2c3")
	}
	_ = c
}

func BenchmarkCellColorPalette(b *testing.B) {
	b.ReportAllocs()
	var c tcell.Color
	for i := 0; i < b.N; i++ {
		c = cellColor("208")
	}
	_ = c
}

func BenchmarkCellStyleRGB(b *testing.B) {
	b.ReportAllocs()
	cell := protocol.Cell{Text: "A", Width: 1, FG: "#ff8800", BG: "#112233"}
	var s tcell.Style
	for i := 0; i < b.N; i++ {
		s = cellStyle(cell)
	}
	_ = s
}
