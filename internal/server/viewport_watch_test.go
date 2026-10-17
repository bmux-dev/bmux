package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"

	"github.com/josephferrero/bmux/internal/protocol"
)

func viewportSession(t *testing.T) (*Server, *session) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := New("/bin/sh")
	t.Cleanup(s.Close)
	if r := s.request(protocol.Request{Op: "create", Name: "view", Dir: t.TempDir(), Width: 100, Height: 30}); r.Error != "" {
		t.Fatal(r.Error)
	}
	return s, s.sessions["view"]
}

func assertSessionSize(t *testing.T, s *session, w, h int) {
	t.Helper()
	f, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true)
	if f.Width != w || f.Height != h {
		t.Fatalf("source workspace = %dx%d, want %dx%d", f.Width, f.Height, w, h)
	}
	for _, p := range f.Panes {
		if len(p.Terminal.Rows) != max(1, p.Height) || len(p.Terminal.Rows[0]) != max(2, p.Width) {
			t.Fatal("source emulator size changed")
		}
		s.mu.Lock()
		pane := s.panes[p.ID]
		s.mu.Unlock()
		rows, cols, err := sizePTY(pane.pty)
		if err != nil || rows != max(1, p.Height) || cols != max(2, p.Width) {
			t.Fatalf("source PTY = %dx%d, error %v", cols, rows, err)
		}
	}
}

func openViewport(t *testing.T, s *Server, r protocol.Request) (net.Conn, *json.Decoder, <-chan struct{}) {
	t.Helper()
	c, peer := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); defer peer.Close(); s.handle(peer) }()
	t.Cleanup(func() {
		c.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("viewport handler leaked")
		}
	})
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	r.Version, r.Op = protocol.Version, "watch-viewport"
	if err := protocol.EncodeRequest(json.NewEncoder(c), r); err != nil {
		t.Fatal(err)
	}
	return c, json.NewDecoder(c), done
}

func readViewport(t *testing.T, dec *json.Decoder) protocol.Response {
	t.Helper()
	var r protocol.Response
	if err := dec.Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestViewportFirstFrameIsImmediateAndDoesNotResizePTY(t *testing.T) {
	server, s := viewportSession(t)
	before := s.status()
	c, dec, done := openViewport(t, server, protocol.Request{Name: "view", Generation: protocol.SessionGeneration(before.Created), Width: 60, Height: 8})
	// A prior resize/settle gate waited at least 250 ms. The first copy now
	// arrives without a refresh tick, even if the app has never repainted.
	_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	r := readViewport(t, dec)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if r.Error != "" || r.Frame == nil || r.Frame.PreviewMode != "terminal-viewport" || r.Frame.Width != 60 || r.Frame.Height != 8 {
		t.Fatalf("bad viewport: %+v", r)
	}
	if len(r.Frame.Panes[0].Terminal.Rows) != 8 || len(r.Frame.Panes[0].Terminal.Rows[0]) != 60 {
		t.Fatal("copied viewport doesn't fit panel")
	}
	assertSessionSize(t, s, 100, 30)
	if after := s.status(); after != before {
		t.Fatal("preview changed attachments, recency, or session metadata")
	}
	if err := s.input(protocol.Request{Op: "resize", Width: 120, Height: 40}); err != nil {
		t.Fatal(err)
	}
	assertSessionSize(t, s, 120, 40)
	c.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disconnect didn't stop reader")
	}
	assertSessionSize(t, s, 120, 40) // No restoration may undo a normal client resize.
}

func TestViewportViewersHaveIndependentPanelSizes(t *testing.T) {
	server, s := viewportSession(t)
	c1, d1, done1 := openViewport(t, server, protocol.Request{Name: "view", Width: 60, Height: 8})
	r1 := readViewport(t, d1)
	c2, d2, _ := openViewport(t, server, protocol.Request{Name: "view", Width: 70, Height: 12})
	r2 := readViewport(t, d2)
	if r1.Frame.Width != 60 || r1.Frame.Height != 8 || r2.Frame.Width != 70 || r2.Frame.Height != 12 {
		t.Fatal("viewers shared presentation geometry")
	}
	assertSessionSize(t, s, 100, 30)
	c1.Close()
	<-done1
	assertSessionSize(t, s, 100, 30)
	c2.Close()
}

func TestViewportSplitAndZoomLayoutFitsWithoutChangingSource(t *testing.T) {
	server, s := viewportSession(t)
	if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	_, dec, _ := openViewport(t, server, protocol.Request{Name: "view", Width: 60, Height: 8})
	r := readViewport(t, dec)
	if len(r.Frame.Panes) != 2 || r.Frame.Panes[1].X+r.Frame.Panes[1].Width != 60 {
		t.Fatal("split presentation doesn't fill panel")
	}
	assertSessionSize(t, s, 100, 30)
	if err := s.input(protocol.Request{Op: "zoom"}); err != nil {
		t.Fatal(err)
	}
	for {
		r = readViewport(t, dec)
		if r.Frame != nil && r.Frame.Zoomed {
			break
		}
	}
	if len(r.Frame.Panes) != 1 || r.Frame.Panes[0].Width != 60 || r.Frame.Panes[0].Height != 8 {
		t.Fatal("zoomed viewport doesn't fill panel")
	}
	assertSessionSize(t, s, 100, 30)
}

func TestViewportRejectsStaleGenerationAndAppInput(t *testing.T) {
	server, s := viewportSession(t)
	_, dec, _ := openViewport(t, server, protocol.Request{Name: "view", Generation: "old", Width: 60, Height: 8})
	if r := readViewport(t, dec); r.Error != "session changed" {
		t.Fatalf("stale target accepted: %+v", r)
	}
	c, dec, done := openViewport(t, server, protocol.Request{Name: "view", Width: 60, Height: 8})
	_ = readViewport(t, dec)
	if err := protocol.EncodeRequest(json.NewEncoder(c), protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("app input did not close read-only stream")
	}
	assertSessionSize(t, s, 100, 30)
	select {
	case <-s.done:
		t.Fatal("viewport injected app input")
	default:
	}
}

func TestViewportHeartbeatLiveOutputAndSessionExit(t *testing.T) {
	server, s := viewportSession(t)
	// Full source width keeps column 0 visible: the viewport follows the
	// cursor horizontally, and CI hostnames can make the prompt very long.
	_, dec, done := openViewport(t, server, protocol.Request{Name: "view", Width: 100, Height: 8})
	_ = readViewport(t, dec)
	for {
		if r := readViewport(t, dec); r.NotModified {
			break
		}
	}
	if err := s.input(protocol.Request{Op: "paste", Text: "printf '\\033[31mVIEWPORT_LIVE\\033[0m\\n'\n"}); err != nil {
		t.Fatal(err)
	}
	last := ""
	for {
		var r protocol.Response
		if err := dec.Decode(&r); err != nil {
			s.mu.Lock()
			var source *pane
			for _, p := range s.panes {
				source = p
			}
			s.mu.Unlock()
			f, _ := source.frame(0)
			t.Fatalf("%v; last viewport:\n%s\nsource pane:\n%s", err, last, frameText(f))
		}
		if r.Frame != nil && len(r.Frame.Panes) > 0 {
			last = frameText(r.Frame.Panes[0].Terminal)
		}
		if strings.Contains(last, "VIEWPORT_LIVE") {
			break
		}
	}
	if err := s.input(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	for {
		r := readViewport(t, dec)
		if r.Frame != nil && len(r.Frame.Panes) == 0 {
			break
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session exit left stream running")
	}
}

func TestViewportSlowReaderIsDisconnectedWithoutResizing(t *testing.T) {
	server, s := viewportSession(t)
	_, dec, done := openViewport(t, server, protocol.Request{Name: "view", Width: 60, Height: 8})
	_ = readViewport(t, dec)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("slow reader retained stream")
	}
	assertSessionSize(t, s, 100, 30)
}

func TestViewportCropsAroundCursorAndPreservesSourceCells(t *testing.T) {
	p := &pane{term: vt.NewEmulator(100, 30), visible: true, revision: 1}
	p.term.SetCallbacks(vt.Callbacks{CursorVisibility: func(visible bool) { p.visible = visible }})
	t.Cleanup(func() { _ = p.term.InputPipe().(io.Closer).Close() })
	p.mu.Lock()
	_, _ = p.term.WriteString("\x1b[?1049h\x1b[2J")
	for y := 0; y < 30; y++ {
		_, _ = p.term.WriteString(fmt.Sprintf("\x1b[%d;81H\x1b[31;44mROW%02d", y+1, y))
	}
	_, _ = p.term.WriteString("\x1b[26;96H\x1b[?25h")
	p.revision++
	p.mu.Unlock()
	f := p.preview(20, 6)
	if !strings.HasPrefix(frameText(f), "ROW23") || f.CursorX != 15 || f.CursorY != 2 || !f.CursorVisible || !f.Alt {
		t.Fatalf("cursor framing incorrect: cursor=%d,%d rows=%q", f.CursorX, f.CursorY, frameText(f))
	}
	if f.Rows[0][0].FG == "" || f.Rows[0][0].BG == "" {
		t.Fatal("source colors were discarded")
	}
	p.mu.Lock()
	_, _ = p.term.WriteString("\x1b[?25l\x1b[1;1HTOP_LEFT")
	p.mu.Unlock()
	f = p.preview(20, 6)
	if f.CursorVisible || !strings.HasPrefix(frameText(f), "TOP_LEFT") {
		t.Fatal("hidden-cursor viewport did not use top-left")
	}
	if p.term.Width() != 100 || p.term.Height() != 30 {
		t.Fatal("viewport changed source emulator dimensions")
	}
}
