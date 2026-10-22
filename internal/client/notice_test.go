package client

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

func noticeUI(t *testing.T, w, h int) (*ui, tcell.SimulationScreen) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(w, h)
	u := &ui{screen: screen, name: "api", settings: config.Default(),
		frame: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("hello terminal")}}}
	return u, screen
}

func TestNoticeBoxIsPlacedWhereTheSettingSays(t *testing.T) {
	const text = "Copied to system clipboard"
	const w, h = 100, 20
	boxW, boxH := displayWidth(text)+4, 3
	for _, position := range config.NoticePositions {
		u, _ := noticeUI(t, w, h)
		u.settings.UI.NoticePosition = position
		x0 := u.paneOriginX(w)
		u.notice(text)
		u.draw()

		x := map[string]int{"left": x0 + 1, "center": x0 + (w-x0-boxW)/2, "right": w - boxW - 1}[position[strings.Index(position, "-")+1:]]
		y := paneOriginY
		if strings.HasPrefix(position, "bottom") {
			y = h - 1 - boxH
		}
		corner, style, _ := u.screen.Get(x, y)
		if fg, _, _ := style.Decompose(); corner != "┌" || fg != accentColor {
			t.Errorf("%s: top-left corner at %d,%d is %q in %v, want ┌ in the accent color", position, x, y, corner, fg)
		}
		if got, _, _ := u.screen.Get(x+boxW-1, y+boxH-1); got != "┘" {
			t.Errorf("%s: bottom-right corner at %d,%d is %q", position, x+boxW-1, y+boxH-1, got)
		}
		if got, _, _ := u.screen.Get(x+2, y+1); got != "C" {
			t.Errorf("%s: text starts with %q", position, got)
		}
		if footer := pickerScreenLine(u, h-1); strings.Contains(footer, "Copied") {
			t.Errorf("%s: notice also in the footer: %q", position, footer)
		}
	}
}

func TestExpiredNoticeIsNotDrawn(t *testing.T) {
	u, _ := noticeUI(t, 100, 20)
	u.notice("gone soon")
	u.messageUntil = time.Now().Add(-time.Second)
	u.draw()
	for y := range 20 {
		if line := pickerScreenLine(u, y); strings.Contains(line, "gone soon") || strings.Contains(line, "┌") && y != 0 {
			t.Fatalf("row %d still shows the notice: %q", y, line)
		}
	}
}

func TestLongNoticeWrapsAndEndsWithAnEllipsis(t *testing.T) {
	lines := noticeText(strings.Repeat("word ", 80), 20)
	if len(lines) != noticeMaxLines || !strings.HasSuffix(lines[len(lines)-1], "…") {
		t.Fatalf("lines: %q", lines)
	}
	for _, line := range lines {
		if displayWidth(line) > 20 {
			t.Fatalf("line wider than the box: %q", line)
		}
	}
	if got := noticeText("short", 20); len(got) != 1 || got[0] != "short" {
		t.Fatalf("short text changed: %q", got)
	}
}

func TestNoticeBoxIsSkippedWhenItCannotFit(t *testing.T) {
	if _, _, ok := noticeBox("top-right", 24, 40, 1, 10, 30, 3); ok {
		t.Fatal("30 columns in a 16-column area")
	}
	if _, _, ok := noticeBox("bottom-left", 0, 80, 1, 3, 20, 3); ok {
		t.Fatal("3 rows in a 2-row area")
	}
	u, _ := noticeUI(t, 12, 6)
	u.notice("hello there")
	u.draw() // must not panic or draw outside the screen
}

func TestSuccessfulCopyClearsTheSelectionAndFailedCopyKeepsIt(t *testing.T) {
	u, sim := noticeUI(t, 50, 12)
	selectHello := func() {
		u.selection = selection{start: point{0, 0}, end: point{4, 0}, active: true}
		u.selection.remember(u.frame)
	}

	selectHello()
	u.clipboard = newClipboardWorker()
	u.clipboard.Close() // Submit now rejects the copy
	u.copy()
	if !u.selection.active || !strings.Contains(u.message, "rejected") {
		t.Fatalf("rejected copy: active %v, notice %q", u.selection.active, u.message)
	}

	u.clipboard = nil
	u.copy()
	if u.selection.active {
		t.Fatal("selection stayed after a successful copy")
	}
	if got := string(sim.GetClipboardData()); got != "hello" {
		t.Fatalf("copied %q", got)
	}

	u.copy()
	if u.message != "Select text by dragging first" {
		t.Fatalf("copy without a selection: %q", u.message)
	}
}

func TestDoubleClickCopyClearsTheSelection(t *testing.T) {
	u, sim := noticeUI(t, 50, 12)
	click := func(buttons tcell.ButtonMask) {
		u.mouse(tcell.NewEventMouse(2, paneOriginY, buttons, tcell.ModNone))
	}
	click(tcell.Button1)
	click(tcell.ButtonNone)
	click(tcell.Button1)
	click(tcell.ButtonNone)
	if u.selection.active {
		t.Fatalf("selection stayed after a double-click copy: %+v", u.selection)
	}
	if got := string(sim.GetClipboardData()); got != "hello" {
		t.Fatalf("double-click copied %q", got)
	}
}
