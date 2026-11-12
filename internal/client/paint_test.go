package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

type paintCountingScreen struct {
	tcell.Screen
	puts, clears, fills int
}

func (s *paintCountingScreen) Put(x, y int, str string, style tcell.Style) (string, int) {
	s.puts++
	return s.Screen.Put(x, y, str, style)
}

func (s *paintCountingScreen) Clear() { s.clears++; s.Screen.Clear() }
func (s *paintCountingScreen) Fill(r rune, style tcell.Style) {
	s.fills++
	s.Screen.Fill(r, style)
}

func TestPaintOnlyWritesCompletedCellChanges(t *testing.T) {
	physical := &paintCountingScreen{Screen: focusTestScreen(t, 12, 3)}
	var paint paintScreen
	style := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	draw := func(text string) {
		paint.begin(physical, 12, 3, tcell.StyleDefault)
		paint.PutStrStyled(0, 1, text, style)
		paint.flush()
		physical.Show()
	}
	draw("hello")
	physical.puts = 0
	draw("hello")
	if physical.puts != 0 {
		t.Fatalf("unchanged frame wrote %d cells", physical.puts)
	}
	draw("hallo")
	if physical.puts != 1 {
		t.Fatalf("one character change wrote %d cells", physical.puts)
	}
	physical.puts = 0
	draw("ha")
	if physical.puts != 3 {
		t.Fatalf("erasing three characters wrote %d cells", physical.puts)
	}
	if physical.clears != 0 || physical.fills != 0 {
		t.Fatal("composition cleared or filled the physical screen")
	}
}

func TestPaintWideGraphemesAndStyleChanges(t *testing.T) {
	physical := &paintCountingScreen{Screen: focusTestScreen(t, 12, 3)}
	var paint paintScreen
	style := tcell.StyleDefault.Foreground(tcell.ColorGreen).Url("https://example.com")
	draw := func(text string, style tcell.Style) {
		paint.begin(physical, 12, 3, tcell.StyleDefault)
		paint.PutStrStyled(0, 1, text, style)
		paint.flush()
		physical.Show()
	}
	draw("界e\u0301", style)
	physical.puts = 0
	draw("界e\u0301", style)
	if physical.puts != 0 {
		t.Fatal("unchanged wide or combining grapheme was rewritten")
	}
	draw("界e\u0301", style.Foreground(tcell.ColorRed))
	if physical.puts != 2 {
		t.Fatalf("style-only change wrote %d cells, want two graphemes", physical.puts)
	}
	draw("a", tcell.StyleDefault)
	for x, want := range []string{"a", " ", " "} {
		if got, _, _ := physical.Get(x, 1); got != want {
			t.Fatalf("wide-to-narrow cell %d = %q, want %q", x, got, want)
		}
	}
	draw("界", style)
	if got, gotStyle, width := physical.Get(0, 1); got != "界" || gotStyle != style || width != 2 {
		t.Fatal("narrow-to-wide change lost text, link, style or width")
	}
	physical.SetSize(5, 2)
	paint.begin(physical, 5, 2, style)
	paint.PutStrStyled(0, 0, "resize", style)
	paint.flush()
	if got, _, _ := physical.Get(4, 0); got != "z" {
		t.Fatalf("resized frame = %q at last column", got)
	}
}

func TestDrawNeverClearsPhysicalScreen(t *testing.T) {
	u := pickerUI(t)
	physical := &paintCountingScreen{Screen: u.screen}
	u.screen = physical
	u.draw()
	u.picker = false
	u.sidebarHidden = true
	u.applyFrame(&protocol.Frame{Name: "test", Focused: 1, Panes: []protocol.PaneFrame{{
		ID: 1, Width: 100, Height: 22,
		Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("content")}},
	}}})
	u.draw()
	if physical.clears != 0 || physical.fills != 0 {
		t.Fatalf("draw cleared %d or filled %d physical frames", physical.clears, physical.fills)
	}
	if u.screen != physical {
		t.Fatal("draw left the composition screen installed for input handling")
	}
}

func TestPreviewTickOnlyDrawsUnobstructedPicker(t *testing.T) {
	u := pickerUI(t)
	u.picker = false
	if u.tickPreview() {
		t.Fatal("idle attachment was redrawn by the preview timer")
	}
	u.picker = true
	u.prompt = "new"
	if u.tickPreview() {
		t.Fatal("picker modal was redrawn by the preview timer")
	}
	u.prompt = ""
	if !u.tickPreview() {
		t.Fatal("picker stopped refreshing its preview")
	}
}
