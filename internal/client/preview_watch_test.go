package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestViewportPreviewStreamCancelsOnPickerTransitions(t *testing.T) {
	// A short private socket path accommodates Darwin's sockaddr_un limit.
	dir, err := os.MkdirTemp("", "hv-fit-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "rpc")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	requests := make(chan protocol.Request, 16)
	closed := make(chan protocol.Request, 16)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				reader := protocol.NewRequestReader(c, time.Second)
				r, err := reader.Read()
				if err != nil {
					return
				}
				requests <- r
				f := &protocol.Frame{Name: r.Name, Generation: r.Generation, PreviewMode: "terminal-viewport", Width: r.Width, Height: r.Height, Panes: []protocol.PaneFrame{{ID: 1, Width: r.Width, Height: r.Height, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("VIEWPORT")}}}}}
				_ = json.NewEncoder(c).Encode(protocol.Response{Version: protocol.Version, Frame: f, Revision: 1})
				_, _ = reader.Read()
				closed <- r
			}()
		}
	}()
	u := pickerUI(t)
	u.socket, u.previews, u.done = socket, make(chan previewResult, 16), make(chan struct{})
	defer close(u.done)
	defer u.stopPreview()
	getRequest := func() protocol.Request {
		t.Helper()
		select {
		case r := <-requests:
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("no viewport preview request")
			return protocol.Request{}
		}
	}
	acceptFrame := func() {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			select {
			case result := <-u.previews:
				u.acceptPreview(result)
				if u.preview != nil && u.preview.PreviewMode == "terminal-viewport" {
					return
				}
			case <-deadline:
				t.Fatal("no viewport preview frame")
			}
		}
	}
	assertClosed := func(name string) {
		t.Helper()
		select {
		case r := <-closed:
			if r.Name != name {
				t.Fatalf("closed %s instead of %s", r.Name, name)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("viewport stream did not close")
		}
	}
	u.requestPreview()
	r := getRequest()
	_, area := pickerAreas(100, 24)
	if r.Op != "watch-viewport" || r.Width != area.w || r.Height != area.h || r.Version != protocol.Version {
		t.Fatalf("wrong sizing request: %+v", r)
	}
	acceptFrame()
	oldEpoch := u.previewEpoch
	u.requestPreview()
	if !u.previewBusy || u.previewEpoch != oldEpoch {
		t.Fatal("unchanged picker restarted stream")
	}
	select {
	case <-requests:
		t.Fatal("duplicate viewport stream")
	default:
	}
	u.pickerKey(runeKey('j'))
	assertClosed("api")
	if r = getRequest(); r.Name != "web" {
		t.Fatal("selection didn't open new viewport stream")
	}
	// An old reader's EOF must not mark the new stream inactive or show an error.
	u.acceptPreview(previewResult{epoch: oldEpoch, name: "api", ended: true, err: net.ErrClosed})
	if !u.previewBusy || u.previewError != "" {
		t.Fatal("stale EOF changed current stream state")
	}
	acceptFrame()
	u.screen.(tcell.SimulationScreen).SetSize(120, 40)
	u.requestPreview()
	assertClosed("web")
	r = getRequest()
	_, area = pickerAreas(120, 40)
	if r.Width != area.w || r.Height != area.h {
		t.Fatal("resized panel sent old dimensions")
	}
	u.pickerKey(runeKey('n'))
	assertClosed("web")
	if u.previewBusy {
		t.Fatal("prompt retained viewport stream")
	}
	u.pickerKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	_ = getRequest()
	u.screen.(tcell.SimulationScreen).SetSize(40, 12)
	u.requestPreview()
	assertClosed("web")
	u.screen.(tcell.SimulationScreen).SetSize(100, 24)
	u.requestPreview()
	_ = getRequest()
	u.picker = false
	u.requestPreview()
	assertClosed("web")
	u.picker = true
	u.requestPreview()
	_ = getRequest()
	// Reuse of a name must not retain the old instance's stream/cache.
	u.sessions[1].Created = time.Now()
	u.requestPreview()
	assertClosed("web")
	r = getRequest()
	if r.Generation != protocol.SessionGeneration(u.sessions[1].Created) || u.preview != nil {
		t.Fatal("replacement session reused old viewport stream or cache")
	}
	u.quitting = true
	u.requestPreview()
	assertClosed("web")
}

func TestViewportPreviewRendersEveryPaneAtNativePanelGeometry(t *testing.T) {
	u := pickerUI(t)
	area := pickerArea{1, 13, 19, 8}
	u.preview = &protocol.Frame{PreviewMode: "terminal-viewport", Width: 19, Height: 8, Focused: 2, Panes: []protocol.PaneFrame{
		{ID: 1, X: 0, Y: 0, Width: 9, Height: 8, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("LEFT")}}},
		{ID: 2, X: 10, Y: 0, Width: 9, Height: 8, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("RIGHT")}, CursorVisible: true, CursorX: 4, CursorY: 0}},
	}}
	u.drawTerminalView(area)
	for _, cell := range []struct {
		x    int
		text string
	}{{area.x, "L"}, {area.x + 10, "R"}, {area.x + 9, "┃"}} {
		text, _, _ := u.screen.Get(cell.x, area.y)
		if text != cell.text {
			t.Fatalf("cell %d = %q, want %q", cell.x, text, cell.text)
		}
	}
	x, y, visible := u.screen.(tcell.SimulationScreen).GetCursor()
	if !visible || x != area.x+14 || y != area.y {
		t.Fatal("focused pane cursor lost its panel coordinates")
	}
}
