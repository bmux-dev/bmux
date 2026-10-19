package server

import (
	"reflect"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestConditionalLivePreviewSendsOnlyChangedFrames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New("/bin/sh")
	defer s.Close()
	if r := s.request(protocol.Request{Op: "create", Name: "live", Dir: t.TempDir()}); r.Error != "" {
		t.Fatal(r.Error)
	}
	r := s.request(protocol.Request{Op: "preview", Kind: "overview", Name: "live"})
	if r.Frame == nil || r.Revision == 0 || r.Frame.Generation == "" {
		t.Fatal("initial preview missing revision/generation")
	}
	generation := r.Frame.Generation
	deadline := time.Now().Add(5 * time.Second)
	for {
		r = s.request(protocol.Request{Op: "preview", Kind: "overview", Name: "live", Revision: r.Revision, Generation: generation})
		if r.NotModified {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle session didn't stop transmitting full snapshots")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Frame != nil {
		t.Fatal("not-modified response included full cell buffer")
	}
	before := s.request(protocol.Request{Op: "list"}).Sessions[0]
	s.mu.Lock()
	item := s.sessions["live"]
	s.mu.Unlock()
	if err := item.input(protocol.Request{Op: "paste", Text: "printf 'LIVE_CHANGED\\n'\n"}); err != nil {
		t.Fatal(err)
	}
	oldRevision := r.Revision
	for {
		r = s.request(protocol.Request{Op: "preview", Kind: "overview", Name: "live", Revision: oldRevision, Generation: generation})
		if r.Frame != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("changed output wasn't delivered to live preview")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.Revision <= oldRevision || r.NotModified {
		t.Fatal("new output didn't advance revision")
	}
	if after := s.request(protocol.Request{Op: "list"}).Sessions[0]; !reflect.DeepEqual(after, before) {
		t.Fatal("live preview changed client count/recency")
	}
	// A different session generation must force a full frame even if the
	// numeric revision happens to coincide with an older session's revision.
	r = s.request(protocol.Request{Op: "preview", Kind: "overview", Name: "live", Revision: r.Revision, Generation: "older-session"})
	if r.Frame == nil || r.NotModified {
		t.Fatal("replacement-session generation reused stale preview")
	}
}
