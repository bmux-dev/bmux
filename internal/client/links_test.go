package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestLinkAt(t *testing.T) {
	osc8 := cells("docs here")
	for i := range 4 {
		osc8[i].Link = 1
	}
	wide := []protocol.Cell{{Text: "界", Width: 2}, {}, {Text: " ", Width: 1}}
	wide = append(wide, cells("https://x.io/a")...)

	for _, tc := range []struct {
		name  string
		f     *protocol.TerminalFrame
		x, y  int
		want  string
		which string
	}{
		{"osc8 cell", &protocol.TerminalFrame{Rows: [][]protocol.Cell{osc8}, Links: []string{"https://example.com/d"}}, 2, 0, "https://example.com/d", ""},
		{"osc8 neighbour is plain text", &protocol.TerminalFrame{Rows: [][]protocol.Cell{osc8}, Links: []string{"https://example.com/d"}}, 6, 0, "", ""},
		{"backslash ends a url", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells(`https://x.io/a\033\\text`)}}, 5, 0, "https://x.io/a", ""},
		{"osc8 with an unsafe scheme", &protocol.TerminalFrame{Rows: [][]protocol.Cell{osc8}, Links: []string{"file:///etc/passwd"}}, 1, 0, "", ""},
		{"osc8 index out of range", &protocol.TerminalFrame{Rows: [][]protocol.Cell{osc8}}, 1, 0, "", ""},
		{"plain url", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("see https://x.io/a?b=1 now")}}, 8, 0, "https://x.io/a?b=1", ""},
		{"before the url", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("see https://x.io/a now")}}, 2, 0, "", ""},
		{"after the url", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("see https://x.io/a now")}}, 19, 0, "", ""},
		{"sentence punctuation", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("go to https://x.io/a.")}}, 10, 0, "https://x.io/a", ""},
		{"clicking the trailing period", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("go to https://x.io/a.")}}, 20, 0, "", ""},
		{"parenthesised", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("(see https://x.io/a)")}}, 8, 0, "https://x.io/a", ""},
		{"balanced parens stay", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("https://en.wikipedia.org/wiki/Go_(game)")}}, 3, 0, "https://en.wikipedia.org/wiki/Go_(game)", ""},
		{"mailto", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("mail mailto:me@x.io please")}}, 7, 0, "mailto:me@x.io", ""},
		{"ftp is not opened", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("ftp://x.io/a")}}, 3, 0, "", ""},
		{"wrapped from the first row", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("see https:"), cells("//x.io/err"), cells("next      ")}, Wrapped: []bool{true, false, false}}, 6, 0, "https://x.io/err", ""},
		{"wrapped from the second row", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("see https:"), cells("//x.io/err"), cells("next      ")}, Wrapped: []bool{true, false, false}}, 3, 1, "https://x.io/err", ""},
		{"unwrapped rows do not join", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("see https:"), cells("//x.io/err")}}, 6, 0, "", ""},
		{"after a wide character", &protocol.TerminalFrame{Rows: [][]protocol.Cell{wide}}, 12, 0, "https://x.io/a", ""},
		{"outside the frame", &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("https://x.io")}}, 40, 0, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkAt(tc.f, tc.x, tc.y); got != tc.want {
				t.Fatalf("linkAt(%d,%d) = %q, want %q", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestOpenableLinkRefusesUnsafeTargets(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://x.io/a":         true,
		"http://localhost:8080/": true,
		"mailto:a@b.io":          true,
		"HTTPS://X.IO":           true,
		"file:///etc/passwd":     false,
		"javascript:alert(1)":    false,
		"https:///nohost":        false,
		"https://x.io/\x1b]52":   false,
		"https://x.io/\a":        false,
		"x-app://open":           false,
		"":                       false,
	} {
		if got := openableLink(raw); got != want {
			t.Errorf("openableLink(%q) = %v, want %v", raw, got, want)
		}
	}
	if err := openLink("file:///etc/passwd"); err == nil {
		t.Error("openLink opened a file: URL")
	}
}

func linkUI(t *testing.T, rows [][]protocol.Cell, links []string, appMouse bool) (*ui, *[]string) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(50, 12)
	opened := &[]string{}
	return &ui{screen: screen, name: "api", frame: &protocol.TerminalFrame{Rows: rows, Links: links, Mouse: appMouse},
		opener: func(s string) error { *opened = append(*opened, s); return nil }}, opened
}

func TestCtrlClickOpensLinkWithoutSelecting(t *testing.T) {
	u, opened := linkUI(t, [][]protocol.Cell{cells("open https://x.io/a now")}, nil, false)
	u.mouse(tcell.NewEventMouse(8, paneOriginY, tcell.Button1, tcell.ModCtrl))
	u.mouse(tcell.NewEventMouse(9, paneOriginY, tcell.Button1, tcell.ModCtrl)) // press held, mouse moved
	u.mouse(tcell.NewEventMouse(9, paneOriginY, tcell.ButtonNone, tcell.ModNone))
	if len(*opened) != 1 || (*opened)[0] != "https://x.io/a" {
		t.Fatalf("opened %v", *opened)
	}
	if u.selection.active || u.selection.dragging || u.linkHeld {
		t.Fatalf("Ctrl+click on a link left selection state: %+v", u.selection)
	}
	// The next click selects as usual.
	u.mouse(tcell.NewEventMouse(0, paneOriginY, tcell.Button1, tcell.ModNone))
	if !u.selection.dragging {
		t.Fatal("plain click after a link click did not select")
	}
	if len(*opened) != 1 {
		t.Fatalf("plain click opened a link: %v", *opened)
	}
}

func TestPlainClickOnLinkStillSelects(t *testing.T) {
	u, opened := linkUI(t, [][]protocol.Cell{cells("open https://x.io/a now")}, nil, false)
	u.mouse(tcell.NewEventMouse(8, paneOriginY, tcell.Button1, tcell.ModNone))
	u.mouse(tcell.NewEventMouse(12, paneOriginY, tcell.Button1, tcell.ModNone))
	if len(*opened) != 0 || u.selection.text() != "ps://" {
		t.Fatalf("opened %v, selected %q", *opened, u.selection.text())
	}
}

func TestCtrlClickOffLinkFallsThrough(t *testing.T) {
	u, opened := linkUI(t, [][]protocol.Cell{cells("open https://x.io/a now")}, nil, false)
	u.mouse(tcell.NewEventMouse(1, paneOriginY, tcell.Button1, tcell.ModCtrl))
	if len(*opened) != 0 || !u.selection.dragging {
		t.Fatalf("Ctrl+click off a link: opened %v, selection %+v", *opened, u.selection)
	}
}

func TestCtrlClickOnLinkNeverReachesTheApplication(t *testing.T) {
	row := cells("docs")
	for i := range row {
		row[i].Link = 1
	}
	u, opened := linkUI(t, [][]protocol.Cell{row}, []string{"https://example.com/d"}, true)
	u.mouse(tcell.NewEventMouse(1, paneOriginY, tcell.Button1, tcell.ModCtrl))
	u.mouse(tcell.NewEventMouse(1, paneOriginY, tcell.ButtonNone, tcell.ModNone))
	if len(*opened) != 1 || (*opened)[0] != "https://example.com/d" {
		t.Fatalf("opened %v", *opened)
	}
	if u.appMouseDown {
		t.Fatal("click was forwarded to the application")
	}
}

func TestOSC8StyleReachesTheOuterTerminal(t *testing.T) {
	row := cells("ab")
	row[0].Link, row[1].Link = 1, 2
	u, _ := linkUI(t, [][]protocol.Cell{row}, []string{"https://x.io/", "file:///etc/passwd"}, false)
	u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 10, Height: 3, Terminal: u.frame}}})
	u.draw()
	ox := u.paneOriginX(50)
	plain := cellStyle(row[0])
	if _, style, _ := u.screen.Get(ox, paneOriginY); style != plain.Url("https://x.io/") {
		t.Fatal("OSC 8 link not passed to the outer terminal")
	}
	if _, style, _ := u.screen.Get(ox+1, paneOriginY); style != plain {
		t.Fatal("unsafe link reached the outer terminal")
	}
}
