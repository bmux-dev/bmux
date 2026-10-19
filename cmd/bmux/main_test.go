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

func TestHelpDescribesActualBehavior(t *testing.T) {
	for _, want := range []string{"Ctrl-a p   Last session", "Terminate a session and all its panes"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("help is missing %q", want)
		}
	}
	for _, stale := range []string{"Previous session", "its shell"} {
		if strings.Contains(helpText, stale) {
			t.Errorf("help still says %q", stale)
		}
	}
}
