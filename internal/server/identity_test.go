package server

import (
	"github.com/josephferrero/bmux/internal/protocol"
	"testing"
	"time"
)

func TestCapturedActionsRejectReplacementAndRenamedTargets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New("/bin/sh")
	defer s.Close()
	dir := t.TempDir()
	if r := s.request(protocol.Request{Op: "create", Name: "target", Dir: dir}); r.Error != "" {
		t.Fatal(r.Error)
	}
	created := s.request(protocol.Request{Op: "list"}).Sessions[0]
	generation := created.Created.Format(time.RFC3339Nano)
	if r := s.request(protocol.Request{Op: "rename", Name: "target", NewName: "original", Generation: generation}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if r := s.request(protocol.Request{Op: "create", Name: "target", Dir: dir}); r.Error != "" {
		t.Fatal(r.Error)
	}
	for _, r := range []protocol.Request{{Op: "kill", Name: "target", Generation: generation}, {Op: "rename", Name: "target", NewName: "oops", Generation: generation}} {
		if response := s.request(r); response.Error == "" {
			t.Fatalf("stale action accepted: %+v", r)
		}
	}
	items := s.request(protocol.Request{Op: "list"}).Sessions
	if len(items) != 2 {
		t.Fatal("replacement or original was terminated")
	}
	for _, item := range items {
		if item.Name == "oops" {
			t.Fatal("replacement was renamed")
		}
	}
	if r := s.request(protocol.Request{Op: "kill", Name: "original", Generation: generation}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if r := s.request(protocol.Request{Op: "kill", Name: "target"}); r.Error != "" {
		t.Fatal("explicit name-based CLI action should still work", r.Error)
	}
}
