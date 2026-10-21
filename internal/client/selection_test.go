package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
	"testing"
)

func cells(text string) []protocol.Cell {
	var row []protocol.Cell
	for _, r := range text {
		row = append(row, protocol.Cell{Text: string(r), Width: 1})
	}
	return row
}

func TestSelectionForwardAndReverse(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		s := selection{start: point{2, 10}, end: point{2, 11}, active: true}
		if reverse {
			s.start, s.end = s.end, s.start
		}
		s.remember(&protocol.TerminalFrame{Base: 10, Rows: [][]protocol.Cell{cells("hello  "), cells("world  ")}})
		if got := s.text(); got != "llo\nwor" {
			t.Fatalf("reverse=%v: %q", reverse, got)
		}
		if !s.contains(3, 10) || !s.contains(1, 11) || s.contains(0, 10) || s.contains(3, 11) {
			t.Fatal("incorrect highlight range")
		}
	}
}

// A row the terminal wrapped continues the line: copying joins it to the
// next without a newline and keeps its trailing space, while a row the
// program ended still ends the copied line.
func TestSelectionJoinsWrappedRows(t *testing.T) {
	s := selection{start: point{0, 0}, end: point{2, 2}, active: true}
	s.remember(&protocol.TerminalFrame{
		Rows:    [][]protocol.Cell{cells("https:"), cells("//x.io"), cells("next  ")},
		Wrapped: []bool{true, false, false},
	})
	if got := s.text(); got != "https://x.io\nnex" {
		t.Fatalf("got %q", got)
	}
	s = selection{start: point{0, 0}, end: point{2, 2}, active: true}
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("ab cd "), cells("ef    "), cells("next  ")}, Wrapped: []bool{true}})
	if got := s.text(); got != "ab cd ef\nnex" {
		t.Fatalf("trailing space of a wrapped row lost: %q", got)
	}
}

func TestSelectionWideCharactersAndCachedRows(t *testing.T) {
	row := []protocol.Cell{{Text: "界", Width: 2}, {}, {Text: "e\u0301", Width: 1}, {Text: " ", Width: 1}}
	s := selection{start: point{0, 0}, end: point{3, 0}, active: true}
	s.remember(&protocol.TerminalFrame{Rows: [][]protocol.Cell{row}})
	s.remember(&protocol.TerminalFrame{Base: 1, Rows: [][]protocol.Cell{cells("new!")}})
	if got := s.text(); got != "界e\u0301" {
		t.Fatalf("got %q", got)
	}
	s.start, s.end = point{1, 0}, point{1, 0}
	if got := s.text(); got != "界" {
		t.Fatalf("selection in continuation cell: %q", got)
	}
}

func TestWordBounds(t *testing.T) {
	lo, hi := wordBounds(cells("cd ~/work/api && go test"), 7)
	if lo != 3 || hi != 12 {
		t.Fatalf("bounds=%d,%d", lo, hi)
	}
}

func TestFuzzySearch(t *testing.T) {
	for _, tc := range []struct {
		q, text string
		match   bool
	}{{"hsa", "hsdp-stl-api", true}, {"API", "~/projects/api", true}, {"zzz", "api", false}, {"", "api", true}, {"界", "世界", true}} {
		_, ok := fuzzyScore(tc.q, tc.text)
		if ok != tc.match {
			t.Errorf("%q in %q: %v", tc.q, tc.text, ok)
		}
	}
}

func TestKeyMapping(t *testing.T) {
	for _, tc := range []struct {
		event *tcell.EventKey
		code  rune
		mod   int
	}{
		{tcell.NewEventKey(tcell.KeyCtrlC, 0, tcell.ModNone), 'c', int(uv.ModCtrl)},
		{tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone), uv.KeyUp, 0},
		{tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone), uv.KeyTab, int(uv.ModShift)},
		{tcell.NewEventKey(tcell.KeyRune, '界', tcell.ModNone), '界', 0},
		{tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModAlt), 'a', int(uv.ModAlt)},
		{tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), uv.KeyEnter, 0},
		{tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModShift), uv.KeyEnter, int(uv.ModShift)},
		{tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl), uv.KeyEnter, int(uv.ModCtrl)},
		{tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModShift|tcell.ModCtrl), uv.KeyUp, int(uv.ModShift | uv.ModCtrl)},
		{tcell.NewEventKey(tcell.KeyRune, 'C', tcell.ModCtrl|tcell.ModShift), 'c', int(uv.ModCtrl)},
	} {
		r := keyRequest(tc.event)
		if r.Code != tc.code || r.Mod != tc.mod {
			t.Errorf("%v => %+v", tc.event, r)
		}
	}
}

func TestModelessSelectionAndPickerRendering(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "simulated")
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(50, 12)
	u := &ui{screen: screen, frame: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("hello terminal")}}, name: "api"}
	u.mouse(tcell.NewEventMouse(0, paneOriginY, tcell.Button1, tcell.ModNone))
	u.mouse(tcell.NewEventMouse(4, paneOriginY, tcell.Button1, tcell.ModNone))
	if got := u.selection.text(); got != "hello" {
		t.Fatalf("drag selection: %q", got)
	}
	u.draw()
	_, style, _ := screen.Get(1, paneOriginY)
	_, bg, _ := style.Decompose()
	if bg != selectionBackground {
		t.Fatal("selection is not highlighted")
	}
	u.mouse(tcell.NewEventMouse(4, 11, tcell.ButtonNone, tcell.ModNone))
	if u.selection.dragging || !u.selection.active {
		t.Fatal("release outside the pane did not end selection")
	}
	u.picker = true
	u.sessions = []protocol.Session{{Name: "api", Dir: "/projects/api"}, {Name: "frontend", Dir: "/projects/web"}}
	u.query = "fr"
	u.draw()
	if matches := u.filtered(); len(matches) != 1 || matches[0].Name != "frontend" {
		t.Fatalf("matches: %+v", matches)
	}
}

func TestConcealedTextIsNotRendered(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(20, 5)
	u := &ui{screen: screen, frame: &protocol.TerminalFrame{Rows: [][]protocol.Cell{{{Text: "x", Width: 1, Attrs: uv.AttrConceal}}}}}
	u.draw()
	text, _, _ := screen.Get(0, paneOriginY)
	if text != " " {
		t.Fatalf("concealed text rendered: %q", text)
	}
}
