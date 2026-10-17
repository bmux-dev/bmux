package server

import (
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestSessionPreviewIsReadOnlyBoundedAndCursorFramed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := New("/bin/sh")
	defer server.Close()
	if r := server.request(protocol.Request{Op: "create", Name: "preview", Dir: t.TempDir(), Width: 81, Height: 25}); r.Error != "" {
		t.Fatal(r.Error)
	}
	server.mu.Lock()
	s := server.sessions["preview"]
	server.mu.Unlock()
	for _, direction := range []string{"horizontal", "vertical"} {
		if err := s.input(protocol.Request{Op: "split", Direction: direction}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.input(protocol.Request{Op: "paste", PaneID: 1, Text: "i=0; while [ $i -lt 35 ]; do echo line-$i; i=$((i+1)); done\n"}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, s.panes[1], func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "line-34") })
	before := s.status()
	sizes := make(map[int][2]int)
	for id, p := range s.panes {
		rows, cols, err := sizePTY(p.pty)
		if err != nil {
			t.Fatal(err)
		}
		sizes[id] = [2]int{cols, rows}
	}
	r := server.request(protocol.Request{Op: "preview", Name: "preview", Width: 41, Height: 9})
	if r.Error != "" || r.Frame == nil || len(r.Frame.Panes) != 3 || r.Frame.Focused != 3 {
		t.Fatalf("preview: %+v", r)
	}
	for _, p := range r.Frame.Panes {
		f := p.Terminal
		if f.CursorVisible && (f.CursorX < 0 || f.CursorX >= p.Width || f.CursorY < 0 || f.CursorY >= p.Height) {
			t.Fatal("source cursor wasn't translated into its preview rectangle")
		}
	}
	if after := s.status(); after != before {
		t.Fatalf("preview changed attachment/recency metadata: %+v -> %+v", before, after)
	}
	for id, p := range s.panes {
		rows, cols, err := sizePTY(p.pty)
		if err != nil {
			t.Fatal(err)
		}
		if [2]int{cols, rows} != sizes[id] {
			t.Fatalf("preview resized pane %d", id)
		}
	}
	if s.focused != 3 || s.zoomed {
		t.Fatal("preview changed focus/zoom")
	}
	full := server.request(protocol.Request{Op: "preview", Kind: "overview", Name: "preview", Width: 10, Height: 4})
	if full.Frame == nil || full.Frame.PreviewMode != "overview" || full.Frame.Width != 81 || full.Frame.Height != 25 || full.Frame.Panes[0].Width != 40 || len(full.Frame.Panes[0].Terminal.Rows) != 25 {
		t.Fatal("overview request returned already-cropped content instead of full terminal state")
	}
	if after := s.status(); after != before {
		t.Fatal("full overview changed session metadata")
	}
	for _, size := range [][2]int{{360, 140}, {81, 25}, {53, 12}, {800, 240}} {
		r = server.request(protocol.Request{Op: "preview", Name: "preview", Width: size[0], Height: size[1]})
		if r.Frame.Width != size[0] || r.Frame.Height != size[1] {
			t.Fatalf("preview didn't fit requested window %v: %dx%d", size, r.Frame.Width, r.Frame.Height)
		}
		right, bottom := 0, 0
		for _, p := range r.Frame.Panes {
			right = max(right, p.X+p.Width)
			bottom = max(bottom, p.Y+p.Height)
		}
		if right != size[0] || bottom != size[1] {
			t.Fatalf("layout didn't fill preview area %v: %dx%d", size, right, bottom)
		}
	}
	for id, p := range s.panes {
		rows, cols, err := sizePTY(p.pty)
		if err != nil {
			t.Fatal(err)
		}
		if [2]int{cols, rows} != sizes[id] {
			t.Fatalf("fitting preview changed pane %d's PTY", id)
		}
	}
	r = server.request(protocol.Request{Op: "preview", Name: "preview", Width: 1000000, Height: 1000000})
	cells := 0
	for _, p := range r.Frame.Panes {
		for _, row := range p.Terminal.Rows {
			cells += len(row)
		}
	}
	nativeCells := 0
	for _, size := range sizes {
		nativeCells += size[0] * size[1]
	}
	if cells > nativeCells || r.Frame.Width > 4096 || r.Frame.Height > 2048 {
		t.Fatalf("unbounded thumbnail: %d cells", cells)
	}
	if err := s.input(protocol.Request{Op: "zoom"}); err != nil {
		t.Fatal(err)
	}
	r = server.request(protocol.Request{Op: "preview", Name: "preview", Width: 41, Height: 9})
	if !r.Frame.Zoomed || len(r.Frame.Panes) != 1 || r.Frame.Panes[0].ID != 3 || !s.zoomed {
		t.Fatal("preview didn't preserve zoomed workspace")
	}
	if r := server.request(protocol.Request{Op: "preview", Name: "missing"}); r.Error == "" {
		t.Fatal("missing session accepted")
	}
}
