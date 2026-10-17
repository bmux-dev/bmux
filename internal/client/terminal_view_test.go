package client

import (
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestNativeTerminalViewIsAlwaysBelowList(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 16}, {40, 12}} {
		list, terminal := pickerAreas(size[0], size[1])
		if terminal.w > 0 && (terminal.y <= list.y+list.h || terminal.x != list.x+1 || terminal.w != list.w-2) {
			t.Fatalf("terminal isn't full-width below list at %v: %+v %+v", size, list, terminal)
		}
	}
}

func TestNativeTerminalCellsAndSourceColorsAreUnchanged(t *testing.T) {
	u := pickerUI(t)
	row := cells("▀ native terminal")
	row[0].FG = "#ff0000"
	row[0].BG = "#123456"
	u.preview = &protocol.Frame{Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: len(row), Height: 1, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{row}}}}}
	area := pickerArea{1, 15, 50, 5}
	u.drawTerminalView(area)
	text, style, _ := u.screen.Get(area.x, area.y)
	fg, bg, _ := style.Decompose()
	if text != "▀" || fg != tcell.GetColor("#ff0000") || bg != tcell.GetColor("#123456") {
		t.Fatal("native source glyph or colors were replaced")
	}
	var line strings.Builder
	for x := 0; x < len(row); x++ {
		c, _, _ := u.screen.Get(area.x+x, area.y)
		line.WriteString(c)
	}
	if line.String() != "▀ native terminal" {
		t.Fatalf("source cells changed: %q", line.String())
	}
}

func TestNativeTerminalViewportKeepsCursorVisibleWithoutReflow(t *testing.T) {
	u := pickerUI(t)
	rows := make([][]protocol.Cell, 20)
	for y := range rows {
		rows[y] = cells("0123456789ABCDEFGHIJ")
	}
	u.preview = &protocol.Frame{Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 20, Height: 20, Terminal: &protocol.TerminalFrame{Rows: rows, CursorVisible: true, CursorX: 18, CursorY: 19, CursorStyle: 2}}}}
	area := pickerArea{1, 15, 10, 5}
	u.drawTerminalView(area)
	text, _, _ := u.screen.Get(area.x, area.y)
	if text != "A" {
		t.Fatal("viewport was reflowed/scaled instead of retaining source cells")
	}
	x, y, visible := u.screen.(tcell.SimulationScreen).GetCursor()
	if !visible || x != area.x+8 || y != area.y+4 {
		t.Fatalf("source cursor outside native viewport: %d,%d,%v", x, y, visible)
	}
	u.searching = true
	u.screen.HideCursor()
	u.drawTerminalView(area)
	_, _, visible = u.screen.(tcell.SimulationScreen).GetCursor()
	if visible {
		t.Fatal("terminal cursor stole focus from picker search")
	}
}

func TestNativePreviewDeduplicatesIdleAndRejectsStaleResults(t *testing.T) {
	u := pickerUI(t)
	u.previewName = "api"
	u.previewEpoch = 4
	f := splitFrame()
	f.PreviewMode = "overview"
	u.preview = f
	result := previewResult{epoch: 4, name: "api", notModified: true, revision: 9}
	u.acceptPreview(result)
	if u.preview != f || u.previewRevision != 9 {
		t.Fatal("idle terminal snapshot was discarded")
	}
	u.acceptPreview(previewResult{epoch: 4, name: "api", err: errors.New("temporary failure")})
	if u.preview != f || !strings.Contains(u.previewError, "stale") {
		t.Fatal("cached terminal state wasn't retained on temporary failure")
	}
	u.acceptPreview(previewResult{epoch: 4, name: "api", frame: f, revision: 10})
	if u.previewError != "" || u.preview != f || u.previewRevision != 10 {
		t.Fatal("live terminal recovery failed")
	}
}
