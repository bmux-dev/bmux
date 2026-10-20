package client

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestUpdateHintUsesOnlyTheBottomBarsFreeSpace(t *testing.T) {
	u := pickerUI(t)
	u.picker = false
	u.sidebarHidden = true
	screen := u.screen.(tcell.SimulationScreen)
	frame := func(hint *protocol.UpdateHint) {
		rows := make([][]protocol.Cell, 24)
		for y := range rows {
			rows[y] = cells("x")
		}
		u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Update: hint, Panes: []protocol.PaneFrame{{ID: 1, Width: 100, Height: 24, Terminal: &protocol.TerminalFrame{Rows: rows}}}})
	}
	bottom := func() string { _, h := u.screen.Size(); return pickerScreenLine(u, h-1) }

	frame(nil)
	screen.SetSize(200, 24)
	u.draw()
	footer := strings.TrimRight(bottom(), " ")
	if strings.ContainsAny(footer, "↑↻") {
		t.Fatalf("hint without a newer release: %q", footer)
	}
	used := len([]rune(footer))

	frame(&protocol.UpdateHint{Version: "0.6.0"})
	for _, c := range []struct {
		width int
		end   string
	}{
		{used + 20, " ↑ 0.6.0 "},
		{used + 6, " ↑ "},                         // no room for the version
		{used + 2, footer[len(footer)-1:] + "  "}, // no room at all: the footer is untouched
	} {
		screen.SetSize(c.width, 24)
		u.draw()
		line := bottom()
		if !strings.HasSuffix(line, c.end) || !strings.HasPrefix(line, footer[:min(len(footer), c.width)]) {
			t.Errorf("width %d: bottom bar %q, want it to end with %q and keep the footer", c.width, line, c.end)
		}
	}

	// A click on the hint says what to do; elsewhere the bar opens the picker.
	screen.SetSize(used+20, 24)
	u.draw()
	u.mouse(tcell.NewEventMouse(used+18, 23, tcell.Button1, tcell.ModNone))
	if u.picker || u.message != "bmux 0.6.0 is available: run bmux update" {
		t.Fatalf("click on the hint: picker %v, message %q", u.picker, u.message)
	}
	if lines := u.helpLines(); lines[0] != u.message {
		t.Fatalf("key list doesn't lead with the hint: %q", lines[0])
	}
	u.mouse(tcell.NewEventMouse(3, 23, tcell.Button1, tcell.ModNone))
	if !u.picker {
		t.Fatal("a click on the rest of the bar no longer opens the picker")
	}
	u.picker = false

	frame(&protocol.UpdateHint{Version: "0.6.0", Installed: true})
	u.draw()
	if !strings.HasSuffix(bottom(), " ↻ 0.6.0 ") {
		t.Fatalf("installed hint: %q", bottom())
	}
	u.mouse(tcell.NewEventMouse(used+18, 23, tcell.Button1, tcell.ModNone))
	if !strings.Contains(u.message, "run bmux restart") {
		t.Fatalf("installed hint message: %q", u.message)
	}
}
