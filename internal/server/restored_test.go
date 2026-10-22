package server

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestRestoredSessionsStayMarkedUntilAttached(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "sessions.json")
	first := persistentServer(t, path)
	if r := first.request(protocol.Request{Op: "create", Name: "old", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	first.Close()

	second := persistentServer(t, path)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- second.Serve(l) }()
	defer func() {
		second.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := second.request(protocol.Request{Op: "create", Name: "fresh", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	restored := func() map[string]bool {
		t.Helper()
		r := second.request(protocol.Request{Op: "list"})
		marks := make(map[string]bool)
		for _, s := range r.Sessions {
			marks[s.Name] = s.Restored
		}
		return marks
	}
	if got := restored(); !got["old"] || got["fresh"] || len(got) != 2 {
		t.Fatalf("before attaching: %v", got)
	}

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "old", Width: 80, Height: 24}); err != nil {
		t.Fatal(err)
	}
	var r protocol.Response
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("attach: %+v %v", r, err)
	}
	if got := restored(); got["old"] {
		t.Fatalf("attached session still marked: %v", got)
	}
	// Detaching doesn't bring the mark back.
	c.Close()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var clients int
		for _, s := range second.request(protocol.Request{Op: "list"}).Sessions {
			if s.Name == "old" {
				clients = s.Clients
			}
		}
		if clients == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client never detached")
		}
	}
	if got := restored(); got["old"] {
		t.Fatalf("detached session marked again: %v", got)
	}
}
