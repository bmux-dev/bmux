package client

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

// fakeRPC answers one-shot requests on a unix socket the way the server does.
func fakeRPC(t *testing.T, answer func(protocol.Request) protocol.Response) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bmux-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			if json.NewDecoder(c).Decode(&r) == nil {
				response := answer(r)
				response.Version = protocol.Version
				_ = json.NewEncoder(c).Encode(response)
			}
			c.Close()
		}
	}()
	return socket
}

func TestClosePaneAsksOnlyWhenAProgramIsInFront(t *testing.T) {
	u, requests := sidebarUI(t)
	command := "opencode"
	u.socket = fakeRPC(t, func(r protocol.Request) protocol.Response {
		if r.Op != "pane-command" || r.Name != "api" || r.PaneID != 1 {
			return protocol.Response{Error: "unexpected request"}
		}
		return protocol.Response{Command: command}
	})
	closeKeys := func() {
		u.prefix = true
		u.key(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	}

	closeKeys()
	noRequest(t, requests)
	u.draw()
	if footer := pickerScreenLine(u, 19); !strings.Contains(footer, "Close the pane running opencode? y closes it") {
		t.Fatalf("footer: %q", footer)
	}
	u.key(tcell.NewEventKey(tcell.KeyRune, 'Y', tcell.ModNone)) // only lowercase y confirms
	noRequest(t, requests)
	if u.closeConfirm != 0 {
		t.Fatal("another key left the question open")
	}

	closeKeys()
	u.handleRawPaste(&rawPasteEvent{Text: "y"})
	noRequest(t, requests)
	if u.closeConfirm != 0 {
		t.Fatal("a paste left the question open")
	}

	closeKeys()
	u.key(tcell.NewEventKey(tcell.KeyRune, 'y', tcell.ModNone))
	if r := nextRequest(t, requests); r.Op != "close-pane" || r.PaneID != 1 {
		t.Fatalf("confirmed close: %+v", r)
	}

	// Only the shell in front: close at once.
	command = ""
	closeKeys()
	if r := nextRequest(t, requests); r.Op != "close-pane" || r.PaneID != 1 {
		t.Fatalf("idle pane close: %+v", r)
	}
	if u.closeConfirm != 0 {
		t.Fatal("an idle pane asked for confirmation")
	}
}

func TestNextAndPreviousSessionFollowSidebarOrder(t *testing.T) {
	now := time.Now()
	sessions := []protocol.Session{ // recency order, as the server lists them
		{Name: "web", Created: now.Add(time.Minute)},
		{Name: "api", Created: now},
		{Name: "db", Created: now.Add(2 * time.Minute)},
	}
	for _, tc := range []struct {
		current string
		delta   int
		want    string
	}{{"api", 1, "web"}, {"web", 1, "db"}, {"db", 1, "api"}, {"api", -1, "db"}, {"web", -1, "api"}, {"gone", 1, ""}} {
		if got := adjacentSession(sessions, tc.current, tc.delta); got != tc.want {
			t.Errorf("from %s by %d: %q, want %q", tc.current, tc.delta, got, tc.want)
		}
	}
	if got := adjacentSession(sessions[:1], "web", 1); got != "" {
		t.Errorf("single session cycled to %q", got)
	}
}
