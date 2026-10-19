package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestAttachExplicitNameAndArgumentValidation(t *testing.T) {
	name, err := attachTarget("missing-socket", []string{"chosen"})
	if err != nil || name != "chosen" {
		t.Fatalf("explicit attachment: %q %v", name, err)
	}
	if _, err = attachTarget("missing-socket", []string{"one", "two"}); err == nil || !strings.Contains(err.Error(), "attach [NAME]") {
		t.Fatal("extra attach arguments accepted")
	}
}

func TestAttachWithoutNameUsesFirstListedSession(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sessions []protocol.Session
		want     string
		empty    bool
	}{
		{"first in server order", []protocol.Session{{Name: "z-first"}, {Name: "a-second"}}, "z-first", false},
		{"single session", []protocol.Session{{Name: "only"}}, "only", false},
		{"no sessions", nil, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "hv-attach-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "rpc")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			request := make(chan protocol.Request, 1)
			go func() {
				c, err := listener.Accept()
				if err != nil {
					return
				}
				defer c.Close()
				var r protocol.Request
				_ = json.NewDecoder(c).Decode(&r)
				request <- r
				_ = json.NewEncoder(c).Encode(protocol.Response{Version: protocol.Version, Sessions: tc.sessions})
			}()
			name, err := attachTarget(socket, nil)
			if tc.empty {
				if err == nil || !strings.Contains(err.Error(), "no sessions available") {
					t.Fatalf("empty sessions: %v", err)
				}
			} else if err != nil || name != tc.want {
				t.Fatalf("target: %q %v", name, err)
			}
			if r := <-request; r.Op != "list" {
				t.Fatalf("unexpected lookup: %+v", r)
			}
		})
	}
}

func TestClientsRefuseToStartInsideAPane(t *testing.T) {
	dir, err := os.MkdirTemp("", "bmux-nest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("BMUX_RUNTIME_DIR", dir)
	t.Setenv("BMUX_SESSION", "api")
	for _, args := range [][]string{nil, {"attach"}, {"attach", "web"}, {"new", "web"}, {"new", "--dir", dir, "web"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "already inside bmux session api") {
			t.Errorf("bmux %v inside a pane: %v", args, err)
		}
	}
	// No server was started for a refused command.
	if _, err := os.Stat(filepath.Join(dir, "server.pid")); !os.IsNotExist(err) {
		t.Fatal("a refused command started a server")
	}
	for _, args := range [][]string{{"new", "--detach", "web"}, {"new", "-detach=true", "web"}, {"list"}, {"kill", "web"}} {
		if startsClient(args) {
			t.Errorf("bmux %v would be refused but doesn't attach", args)
		}
	}
}

// Agent hooks and extensions report through these commands; losing one from
// the command table silently stops every integration.
func TestIntegrationCommandsDispatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "bmux-dispatch-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("BMUX_RUNTIME_DIR", dir)
	t.Setenv("BMUX_SESSION", "")
	if err := run([]string{"agent", "working", "omp"}); err == nil || !strings.Contains(err.Error(), "not inside a bmux pane") {
		t.Fatalf("bmux agent outside a pane: %v", err)
	}
	if err := run([]string{"integration", "hook", "claude"}); err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("bmux integration hook: %v", err)
	}
}
