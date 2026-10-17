package server

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func snapshot(s *session) *protocol.Frame {
	f, _ := s.snapshot(make(map[int]int), make(map[int]int), 0, true)
	return f
}

func TestSplitsFocusZoomAndIndependentShells(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("workspace", t.TempDir(), "/bin/sh", 81, 25)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	input := func(r protocol.Request) {
		t.Helper()
		if err := s.input(r); err != nil {
			t.Fatal(err)
		}
	}
	input(protocol.Request{Op: "split", Direction: "horizontal"})
	input(protocol.Request{Op: "split", Direction: "vertical"})
	f := snapshot(s)
	if len(f.Panes) != 3 || f.Focused != 3 || f.Panes[0].Width != 40 || f.Panes[1].Height != 12 {
		t.Fatalf("nested split: %+v", f)
	}
	if s.status().Panes != 3 {
		t.Fatal("picker pane count not updated")
	}
	input(protocol.Request{Op: "paste", PaneID: 1, Text: "ONLY_LEFT=left; printf 'LEFT_READY\\n'\n"})
	input(protocol.Request{Op: "paste", PaneID: 3, Text: "printf 'ISOLATED=%s\\n' \"${ONLY_LEFT-unset}\"\n"})
	awaitFrame(t, s.panes[1], func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "LEFT_READY") })
	awaitFrame(t, s.panes[3], func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "ISOLATED=unset") })
	input(protocol.Request{Op: "select-pane", Direction: "up"})
	if s.focused != 2 {
		t.Fatal("up did not select upper-right pane")
	}
	input(protocol.Request{Op: "select-pane", Direction: "left"})
	if s.focused != 1 {
		t.Fatal("left did not select left pane")
	}
	input(protocol.Request{Op: "zoom"})
	f = snapshot(s)
	if !f.Zoomed || len(f.Panes) != 1 || f.Panes[0].Width != 81 || f.Panes[0].Height != 25 {
		t.Fatalf("zoom: %+v", f)
	}
	input(protocol.Request{Op: "resize", Width: 101, Height: 31})
	input(protocol.Request{Op: "zoom"})
	f = snapshot(s)
	if f.Zoomed || len(f.Panes) != 3 || f.Panes[0].Width != 50 {
		t.Fatalf("unzoom lost layout: %+v", f)
	}
	input(protocol.Request{Op: "resize-pane", Direction: "right"})
	f = snapshot(s)
	if f.Panes[0].Width != 55 {
		t.Fatalf("resize not applied: %+v", f.Panes[0])
	}
	input(protocol.Request{Op: "resize", Width: 2, Height: 1})
	if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err == nil {
		t.Fatal("accepted split in tiny terminal")
	}
	input(protocol.Request{Op: "zoom"})
	f = snapshot(s)
	if len(f.Panes) != 1 || f.Panes[0].Width != 2 {
		t.Fatal("cannot zoom after terminal shrinks")
	}
}

func TestPaneScrollbackIsIndependent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("scroll", t.TempDir(), "/bin/sh", 81, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	if err = s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 2; id++ {
		if err = s.input(protocol.Request{Op: "paste", PaneID: id, Text: "i=0; while [ $i -lt 25 ]; do echo line-$i; i=$((i+1)); done\n"}); err != nil {
			t.Fatal(err)
		}
		awaitFrame(t, s.panes[id], func(f *protocol.TerminalFrame) bool {
			return f.History > 10 && strings.Contains(frameText(f), "line-24")
		})
	}
	offsets, histories := map[int]int{}, map[int]int{}
	_, rev := s.snapshot(offsets, histories, 0, true)
	offsets[1] = 5
	f, _ := s.snapshot(offsets, histories, rev, true)
	if f.Panes[0].Terminal.Offset != 5 || f.Panes[1].Terminal.Offset != 0 {
		t.Fatal("scrolling leaked into another pane")
	}
	if err = s.input(protocol.Request{Op: "select-pane", PaneID: 1}); err != nil {
		t.Fatal(err)
	}
	f, _ = s.snapshot(offsets, histories, rev, true)
	if f.Panes[0].Terminal.Offset != 5 {
		t.Fatal("focus switch lost scrollback position")
	}
}

func TestSplitLayoutSurvivesDetachAndReattach(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- s.Serve(l) }()
	defer func() {
		s.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := s.request(protocol.Request{Op: "create", Name: "split", Dir: t.TempDir(), Width: 81, Height: 25}); r.Error != "" {
		t.Fatal(r.Error)
	}
	connect := func() (net.Conn, *json.Encoder, *json.Decoder) {
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		e, d := json.NewEncoder(c), json.NewDecoder(c)
		if err = e.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "split", Width: 81, Height: 25}); err != nil {
			t.Fatal(err)
		}
		var r protocol.Response
		if err = d.Decode(&r); err != nil || r.Error != "" {
			t.Fatalf("attach: %+v %v", r, err)
		}
		return c, e, d
	}
	c, e, d := connect()
	if err = e.Encode(protocol.Request{Op: "split", Direction: "horizontal", Seq: 1}); err != nil {
		t.Fatal(err)
	}
	for {
		var r protocol.Response
		if err = d.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Frame != nil && len(r.Frame.Panes) == 2 {
			if r.Ack != 1 {
				t.Fatal("split frame not acknowledged")
			}
			break
		}
	}
	if err = e.Encode(protocol.Request{Op: "detach"}); err != nil {
		t.Fatal(err)
	}
	c.Close()
	c, _, d = connect()
	defer c.Close()
	var r protocol.Response
	if err = d.Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.Frame == nil || len(r.Frame.Panes) != 2 || r.Frame.Focused != 2 {
		t.Fatalf("layout not preserved: %+v", r)
	}
}
