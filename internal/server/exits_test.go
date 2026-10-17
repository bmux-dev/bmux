package server

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func waitPaneCount(t *testing.T, s *session, count int) *protocol.Frame {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.status().Panes == count {
			return snapshot(s)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wanted %d panes, got %d", count, s.status().Panes)
	return nil
}

func TestExitedPaneCollapsesNestedLayoutAndMovesFocus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("exits", t.TempDir(), "/bin/sh", 81, 25)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	for _, direction := range []string{"horizontal", "vertical"} {
		if err = s.input(protocol.Request{Op: "split", Direction: direction}); err != nil {
			t.Fatal(err)
		}
	}
	offsets, histories := map[int]int{}, map[int]int{}
	_, revision := s.snapshot(offsets, histories, 0, true)
	if err = s.input(protocol.Request{Op: "zoom"}); err != nil {
		t.Fatal(err)
	}
	if err = s.input(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	f := waitPaneCount(t, s, 2)
	if f.Focused != 2 || f.Zoomed || len(f.Panes) != 2 || f.Panes[1].Height != 25 || f.Panes[1].Y != 0 {
		t.Fatalf("focused zoomed exit: %+v", f)
	}
	f, newRevision := s.snapshot(offsets, histories, revision, false)
	if f == nil || newRevision <= revision {
		t.Fatal("pane removal lost its snapshot revision")
	}
	// Exiting an unfocused pane preserves focus and expands its sibling.
	if err = s.input(protocol.Request{Op: "paste", PaneID: 1, Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	f = waitPaneCount(t, s, 1)
	if f.Focused != 2 || f.Panes[0].Width != 81 || f.Panes[0].Height != 25 || f.Panes[0].X != 0 {
		t.Fatalf("unfocused exit: %+v", f)
	}
	if err = s.input(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("last exit did not finish session")
	}
	f = snapshot(s)
	if len(f.Panes) != 0 || f.Focused != 0 || f.Zoomed {
		t.Fatalf("last exit: %+v", f)
	}
	if err = s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err == nil {
		t.Fatal("closed session accepted a split")
	}
}

func TestDetachedExitRemovesRenamedSessionAndAllowsNameReuse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New("/bin/sh")
	defer s.Close()
	dir := t.TempDir()
	if r := s.request(protocol.Request{Op: "create", Name: "before", Dir: dir}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s.mu.Lock()
	item := s.sessions["before"]
	s.mu.Unlock()
	if r := s.request(protocol.Request{Op: "rename", Name: "before", NewName: "after"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if err := item.input(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-item.done:
	case <-time.After(5 * time.Second):
		t.Fatal("detached session did not finish")
	}
	if r := s.request(protocol.Request{Op: "list"}); len(r.Sessions) != 0 {
		t.Fatalf("dead renamed session listed: %+v", r)
	}
	if r := s.request(protocol.Request{Op: "create", Name: "after", Dir: dir}); r.Error != "" {
		t.Fatalf("session name not reusable: %s", r.Error)
	}
	if r := s.request(protocol.Request{Op: "list"}); len(r.Sessions) != 1 || r.Sessions[0].Name != "after" {
		t.Fatalf("replacement removed by old cleanup: %+v", r)
	}
}

func TestLastExitNotifiesAllAttachedClients(t *testing.T) {
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
	if r := s.request(protocol.Request{Op: "create", Name: "shared", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	attach := func() (net.Conn, *json.Encoder, *json.Decoder) {
		c, err := net.Dial("tcp", l.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		e, d := json.NewEncoder(c), json.NewDecoder(c)
		if err = e.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "shared"}); err != nil {
			t.Fatal(err)
		}
		var r protocol.Response
		if err = d.Decode(&r); err != nil || r.Error != "" {
			t.Fatalf("attach: %+v %v", r, err)
		}
		return c, e, d
	}
	a, e, first := attach()
	defer a.Close()
	b, _, second := attach()
	defer b.Close()
	if err = e.Encode(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*json.Decoder{first, second} {
		for {
			var r protocol.Response
			if err = d.Decode(&r); err != nil {
				t.Fatal(err)
			}
			if r.Frame != nil && len(r.Frame.Panes) == 0 {
				break
			}
		}
	}
	if r := s.request(protocol.Request{Op: "list"}); len(r.Sessions) != 0 {
		t.Fatal("empty session still listed")
	}
}
