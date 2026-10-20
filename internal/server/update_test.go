package server

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func resetReleases(t *testing.T) {
	t.Helper()
	clear := func() {
		releases.mu.Lock()
		releases.current, releases.latest, releases.installed = "", "", ""
		releases.mu.Unlock()
		updateHint.Store(nil)
	}
	clear()
	t.Cleanup(clear)
}

// checkOnce runs one WatchReleases round: with stop already closed it
// checks, then returns.
func checkOnce(srv *Server, current string, latest func(context.Context) (string, error)) {
	stop := make(chan struct{})
	close(stop)
	srv.WatchReleases(current, latest, time.Hour, stop)
}

func TestUpdateHintReachesFramesAndFollowsTheSettings(t *testing.T) {
	resetReleases(t)
	s := editorNavigationSession(t)
	srv := New("/bin/sh")
	srv.sessions["navigation"] = s
	path := useConfig(t, srv, "")
	frame := func(last uint64) (*protocol.Frame, uint64) {
		return s.snapshot(map[int]int{}, map[int]int{}, last, false)
	}
	_, revision := s.snapshot(map[int]int{}, map[int]int{}, 0, true)

	calls := 0
	offer := func(v string) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { calls++; return v, nil }
	}
	checkOnce(srv, "0.5.0", offer("0.6.0"))
	f, next := frame(revision)
	if f == nil || f.Update == nil || *f.Update != (protocol.UpdateHint{Version: "0.6.0"}) {
		t.Fatalf("a newer release didn't reach idle clients: %+v", f)
	}
	revision = next

	// The same or an older release is no hint.
	for _, v := range []string{"0.5.0", "0.4.0", ""} {
		checkOnce(srv, "0.5.0", offer(v))
		if f, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true); f.Update != nil {
			t.Fatalf("release %q offered to 0.5.0: %+v", v, f.Update)
		}
	}
	checkOnce(srv, "0.5.0", offer("0.6.0"))

	// bmux update reports the install; that wins and asks for a restart.
	if r := srv.request(protocol.Request{Op: "update-installed", Text: "0.6.0"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if f, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true); f.Update == nil || *f.Update != (protocol.UpdateHint{Version: "0.6.0", Installed: true}) {
		t.Fatalf("installed update: %+v", f.Update)
	}
	if r := srv.request(protocol.Request{Op: "update-installed", Text: "../../etc"}); r.Error == "" {
		t.Fatal("accepted a non-version as installed")
	}
	releases.mu.Lock()
	releases.installed = ""
	releases.mu.Unlock()

	// update.check = false: no request, and the available hint goes away
	// on reload.
	if err := os.WriteFile(path, []byte("[update]\ncheck = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := srv.request(protocol.Request{Op: "reload-config"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if f, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true); f.Update != nil {
		t.Fatalf("hint kept after turning checks off: %+v", f.Update)
	}
	calls = 0
	checkOnce(srv, "0.5.0", offer("0.7.0"))
	if calls != 0 {
		t.Fatalf("checked for releases %d times with update.check = false", calls)
	}
}
