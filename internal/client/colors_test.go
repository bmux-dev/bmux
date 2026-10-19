package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestCellStyleUsesTerminalPalette(t *testing.T) {
	for _, tc := range []struct {
		name string
		want tcell.Color
	}{
		{"1", tcell.PaletteColor(1)},
		{"208", tcell.PaletteColor(208)},
		{"#800000", tcell.NewHexColor(0x800000)},
		{"256", tcell.ColorDefault},
	} {
		fg, _, _ := cellStyle(protocol.Cell{FG: tc.name}).Decompose()
		_, bg, _ := cellStyle(protocol.Cell{BG: tc.name}).Decompose()
		if fg != tc.want || bg != tc.want {
			t.Errorf("color %q = fg %v bg %v, want %v", tc.name, fg, bg, tc.want)
		}
	}
}
