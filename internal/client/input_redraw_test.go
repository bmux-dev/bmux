package client

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

type inputCountingScreen struct {
	tcell.Screen
	shows int
}

func (s *inputCountingScreen) Show() { s.shows++; s.Screen.Show() }

func inputRedrawUI(t *testing.T) (*ui, *inputCountingScreen, <-chan protocol.Request) {
	t.Helper()
	u, requests := navigationUI(t)
	screen := &inputCountingScreen{Screen: focusTestScreen(t, 100, 24)}
	u.screen, u.sidebarHidden = screen, true
	u.applyFrame(&protocol.Frame{Name: "input", Focused: 1, Panes: []protocol.PaneFrame{
		{ID: 1, Width: 10, Height: 20, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("content")}}},
		{ID: 2, X: 11, Width: 10, Height: 20, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("other")}}},
	}})
	u.draw()
	screen.shows = 0
	return u, screen, requests
}

func TestInputDispatchDoesNotDrawForwardedKeys(t *testing.T) {
	for _, event := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModAlt),
	} {
		t.Run(event.Name(), func(t *testing.T) {
			u, screen, requests := inputRedrawUI(t)
			// An existing notice does not make every forwarded key redraw.
			u.notice("existing notice")
			u.handleEvent(event)
			r := nextNavigationRequest(t, requests, "key", "")
			want := keyRequest(event)
			if r.Code != want.Code || r.Text != want.Text || r.Mod != want.Mod || r.PaneID != 1 {
				t.Fatalf("forwarded key = %+v, want %+v to pane 1", r, want)
			}
			if screen.shows != 0 {
				t.Fatalf("forward-only key drew %d times", screen.shows)
			}
			// A subsequent frame still uses the renderer normally.
			f := u.workspace
			f.Panes[0].Terminal.Rows = [][]protocol.Cell{cells("echo")}
			u.applyFrame(f)
			u.draw()
			if text, _, _ := screen.Get(0, paneOriginY); text != "e" || screen.shows != 1 {
				t.Fatalf("response did not render echo: text=%q, draws=%d", text, screen.shows)
			}
		})
	}
}

func TestInputDispatchDrawsLocalKeyChanges(t *testing.T) {
	for _, name := range []string{"selection", "scrollback", "resize hint", "expired resize hint"} {
		t.Run(name, func(t *testing.T) {
			u, screen, requests := inputRedrawUI(t)
			switch name {
			case "selection":
				u.selection = selection{active: true, start: point{0, 0}, end: point{1, 0}}
			case "scrollback":
				u.frame.Offset = 3
			case "resize hint":
				u.resizeUntil = time.Now().Add(time.Second)
			case "expired resize hint":
				u.resizeUntil = time.Now().Add(-time.Second)
			}
			u.draw()
			screen.shows = 0
			u.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
			if name == "scrollback" {
				if r := nextNavigationRequest(t, requests, "scroll", ""); r.Delta != 0 {
					t.Fatalf("did not request live output: %+v", r)
				}
			}
			nextNavigationRequest(t, requests, "key", "")
			if screen.shows != 1 || u.selection.active || !u.resizeUntil.IsZero() {
				t.Fatalf("local change was not drawn: draws=%d, selection=%v, resize=%v", screen.shows, u.selection.active, u.resizeUntil)
			}
			if name == "selection" {
				if _, style, _ := screen.Get(0, paneOriginY); style != cellStyle(u.frame.Rows[0][0]) {
					t.Fatal("selection highlight was not erased")
				}
			}
		})
	}
}

func TestInputDispatchDrawsBindingsAndModals(t *testing.T) {
	u, screen, requests := inputRedrawUI(t)
	u.handleEvent(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	if !u.prefix || screen.shows != 1 {
		t.Fatal("prefix hint did not draw")
	}
	u.handleEvent(tcell.NewEventKey(tcell.KeyRune, '€', tcell.ModNone))
	if u.prefix || u.message == "" || screen.shows != 2 {
		t.Fatal("unknown prefix key did not draw its notice")
	}
	u.help = true
	u.handleEvent(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.help || screen.shows != 3 {
		t.Fatal("closing help did not draw")
	}
	u.prompt, u.renameWindow = "rename-window", 1
	u.handleEvent(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	if u.input != "x" || screen.shows != 4 {
		t.Fatal("editing a prompt did not draw")
	}
	select {
	case r := <-requests:
		t.Fatalf("local input reached the app: %+v", r)
	default:
	}
}

func TestInputDispatchDoesNotDrawForwardedWheels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		app     bool
		mods    tcell.ModMask
		buttons tcell.ButtonMask
		op      string
		button  int
		delta   int
	}{
		{"app up", true, tcell.ModNone, tcell.WheelUp, "mouse", int(uv.MouseWheelUp), 0},
		{"app down", true, tcell.ModNone, tcell.WheelDown, "mouse", int(uv.MouseWheelDown), 0},
		{"history up", false, tcell.ModNone, tcell.WheelUp, "scroll", 0, 3},
		{"history down", false, tcell.ModNone, tcell.WheelDown, "scroll", 0, -3},
		{"shift bypasses app", true, tcell.ModShift, tcell.WheelUp, "scroll", 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, screen, requests := inputRedrawUI(t)
			u.frame.Mouse = tc.app
			u.handleEvent(tcell.NewEventMouse(1, paneOriginY, tc.buttons, tc.mods))
			r := nextNavigationRequest(t, requests, tc.op, "")
			if r.PaneID != 1 || r.Button != tc.button || r.Delta != tc.delta || tc.op == "mouse" && (r.Kind != "wheel" || r.X != 1 || r.Y != 0) {
				t.Fatalf("wheel routing changed: %+v", r)
			}
			if screen.shows != 0 {
				t.Fatalf("forward-only wheel drew %d times", screen.shows)
			}
		})
	}
}

func TestInputDispatchDrawsLocalWheelChanges(t *testing.T) {
	for _, name := range []string{"pane focus", "sidebar focus", "resize hint", "selection drag"} {
		t.Run(name, func(t *testing.T) {
			u, screen, requests := inputRedrawUI(t)
			x := 1
			switch name {
			case "pane focus":
				x = 12
			case "sidebar focus":
				u.sidebarFocus = true
			case "resize hint":
				u.resizeUntil = time.Now().Add(time.Second)
			case "selection drag":
				u.selection = selection{active: true, dragging: true}
			}
			u.handleEvent(tcell.NewEventMouse(x, paneOriginY, tcell.WheelUp, tcell.ModNone))
			if name == "pane focus" {
				if r := nextNavigationRequest(t, requests, "select-pane", ""); r.PaneID != 2 {
					t.Fatalf("wheel selected wrong pane: %+v", r)
				}
			}
			nextNavigationRequest(t, requests, "scroll", "")
			want := 1
			if name == "selection drag" {
				// Scrolling an existing drag does not change its highlight until
				// the next frame arrives; it must not clear that selection.
				want = 0
				if !u.selection.active || !u.selection.dragging {
					t.Fatal("wheel cleared the selection drag")
				}
			}
			if screen.shows != want {
				t.Fatalf("wheel drew %d times, want %d", screen.shows, want)
			}
		})
	}
}

func TestInputDispatchDrawsForwardingErrors(t *testing.T) {
	for _, event := range []tcell.Event{
		tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone),
		tcell.NewEventMouse(1, paneOriginY, tcell.WheelUp, tcell.ModNone),
	} {
		u, screen, _ := inputRedrawUI(t)
		if err := u.conn.Close(); err != nil {
			t.Fatal(err)
		}
		u.handleEvent(event)
		if !strings.HasPrefix(u.message, "Connection: ") || screen.shows != 1 {
			t.Fatalf("send error did not draw: message=%q, draws=%d", u.message, screen.shows)
		}
	}
}

func TestInputDispatchDrawsMixedWheelAndButton(t *testing.T) {
	u, screen, requests := inputRedrawUI(t)
	u.view = scrollView{active: true, pane: 1}
	u.handleEvent(tcell.NewEventMouse(1, paneOriginY, tcell.WheelUp|tcell.Button1, tcell.ModNone))
	nextNavigationRequest(t, requests, "scroll", "") // closing the keyboard view
	nextNavigationRequest(t, requests, "scroll", "") // the wheel
	if u.view.active || screen.shows != 1 {
		t.Fatal("mixed wheel/button event did not redraw the closed keyboard view")
	}
}

func TestInputDispatchKeepsPasteAndFocusDraws(t *testing.T) {
	u, screen, requests := inputRedrawUI(t)
	u.handleEvent(&rawPasteEvent{Text: "pasted"})
	if r := nextNavigationRequest(t, requests, "paste", ""); r.Text != "pasted" || screen.shows != 1 {
		t.Fatalf("paste dispatch changed: request=%+v, draws=%d", r, screen.shows)
	}
	u.handleEvent(tcell.NewEventFocus(false))
	if r := nextNavigationRequest(t, requests, "focus", ""); r.Focused || screen.shows != 2 {
		t.Fatalf("focus dispatch changed: request=%+v, draws=%d", r, screen.shows)
	}
}
