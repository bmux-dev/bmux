package server

import (
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Requests that only arrange and read panes. Ops that type into a shell
// (key, paste, mouse, run-shell, send-input, layout commands with a command)
// are left out: random bytes typed into a real shell could run anything.
var (
	fuzzSessionOps = []string{
		"navigate-pane", "close-pane", "close-window", "resize", "split", "new-window",
		"select-window", "rename-window", "select-pane", "focus-pane", "resize-pane",
		"drag-divider", "swap-pane", "rotate-window", "select-layout", "last-pane",
		"break-pane", "join-pane", "zoom",
	}
	fuzzServerOps = []string{
		"describe", "capture-pane", "history-text", "search-history", "jump-prompt",
		"layout-command", "pane-command", "zoomed", "preview", "list",
	}
	fuzzDirections = []string{"", "left", "right", "up", "down", "horizontal", "vertical", "next", "previous", "sideways"}
	fuzzKinds      = []string{"", "split", "new-window", "select", "kill-pane", "kill-window", "resize", "zoom", "rename-window", "window", "pane", "session", "all", "overview", "even-horizontal", "tiled", "bogus"}
)

// FuzzSessionRequests sends a session a sequence of requests with odd pane,
// window, size and direction values, as a confused client or script might. No
// request may panic the server, and after each one the session must still be
// consistent: every window has panes, each window's focused pane is one of
// its own, and every pane in a layout exists.
func FuzzSessionRequests(f *testing.F) {
	f.Add([]byte{4, 2, 1, 0, 0, 5, 0, 4, 3, 0, 1, 0, 0, 1, 1, 2, 0, 0})               // split, split, close
	f.Add([]byte{5, 0, 0, 0, 0, 0, 0, 16, 1, 9, 0, 0, 0, 0, 13, 2, 0, 0, 0, 0, 0, 0}) // new window, break, join
	f.Add([]byte{3, 0, 255, 255, 0, 0, 18, 0, 0, 0, 0, 0, 11, 5, 0, 0, 120, 3})       // huge resize, zoom, drag
	f.Add([]byte{19, 1, 0, 3, 0, 0, 25, 4, 9, 0, 0, 0, 24, 0, 0, 0, 0, 7})            // server ops on odd targets
	f.Fuzz(func(t *testing.T, data []byte) {
		srv, s := viewportSession(t)
		step := func(i int) byte {
			if i < len(data) {
				return data[i]
			}
			return 0
		}
		// Six bytes a request; at most a dozen, since splits start real shells.
		for i := 0; i+6 <= len(data) && i < 12*6; i += 6 {
			op, pane, window, size, dir, kind := int(step(i)), int(step(i+1)), int(step(i+2)), int(int8(step(i+3))), int(step(i+4)), int(step(i+5))
			r := protocol.Request{
				PaneID: pane%8 - 1, Window: window%6 - 1, NeighborID: pane % 5,
				Width: size * 3, Height: size, Delta: size, X: size, Y: -size,
				Direction: fuzzDirections[dir%len(fuzzDirections)], Kind: fuzzKinds[kind%len(fuzzKinds)],
				NewName: "w", Name: "view",
			}
			if op%(len(fuzzSessionOps)+len(fuzzServerOps)) < len(fuzzSessionOps) {
				r.Op = fuzzSessionOps[op%len(fuzzSessionOps)]
				_ = s.input(r)
			} else {
				r.Op = fuzzServerOps[(op-len(fuzzSessionOps))%len(fuzzServerOps)]
				_ = srv.request(r)
			}
			checkSessionConsistent(t, s, r)
		}
	})
}

func checkSessionConsistent(t *testing.T, s *session, after protocol.Request) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || len(s.panes) == 0 {
		return // its last pane closed: the session ended
	}
	if len(s.windows) == 0 || s.active < 0 || s.active >= len(s.windows) {
		t.Fatalf("after %+v: %d windows, active %d", after, len(s.windows), s.active)
	}
	for i, w := range s.windows {
		if w.root == nil {
			t.Fatalf("after %+v: window %d has no layout", after, i+1)
		}
		var ids []int
		w.root.leaves(&ids)
		focused := false
		for _, id := range ids {
			if s.panes[id] == nil {
				t.Fatalf("after %+v: window %d lays out pane %d, which doesn't exist", after, i+1, id)
			}
			focused = focused || id == w.focused
		}
		if !focused {
			t.Fatalf("after %+v: window %d focuses pane %d, not one of its %v", after, i+1, w.focused, ids)
		}
	}
}
