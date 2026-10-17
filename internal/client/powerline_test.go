package client

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestPickerPowerlineTmuxRowsAndFramedTerminal(t *testing.T) {
	u := pickerUI(t)
	u.preview, u.previewName = splitFrame(), "api"
	u.drawPicker(100, 24)
	list, terminal := pickerAreas(100, 24)
	if list.x != 0 || list.y != 1 || terminal.y < 12 || terminal.w != 98 {
		t.Fatal("picker layout isn't top list / lower framed terminal")
	}
	if !strings.Contains(pickerScreenLine(u, 1), "(0) + api: 2 panes") {
		t.Fatal("missing tmux-style numbered row")
	}
	_, firstStyle, _ := u.screen.Get(0, 1)
	_, lastStyle, _ := u.screen.Get(99, 1)
	if firstStyle != lastStyle {
		t.Fatal("selected highlight does not extend across full row")
	}
	corner, _, _ := u.screen.Get(0, terminal.y-1)
	if corner != "┌" {
		t.Fatalf("terminal border missing: %q", corner)
	}
	if !strings.Contains(pickerScreenLine(u, terminal.y-1), "api · 1/3") {
		t.Fatal("selected session border title missing")
	}
	top := pickerScreenLine(u, 0)
	if !strings.Contains(top, "") || !strings.Contains(top, "") {
		t.Fatal("powerline separators missing")
	}
}

func TestPowerlineASCIIFallbackAndTinyWindows(t *testing.T) {
	t.Setenv("BMUX_POWERLINE_ASCII", "1")
	u := pickerUI(t)
	for _, w := range []int{1, 10, 40, 80, 120} {
		u.screen.(tcell.SimulationScreen).SetSize(w, 24)
		u.drawPowerline(w, time.Unix(0, 0))
		line := pickerScreenLine(u, 0)
		if strings.Contains(line, "") || strings.Contains(line, "") {
			t.Fatal("forced fallback emitted powerline glyphs")
		}
	}
}
