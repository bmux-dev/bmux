package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/server"
)

func TestDirectorySessionNamesAreValidAndKeepUTF8(t *testing.T) {
	for _, tc := range []struct{ dir, want string }{
		{"/projects/api", "api"}, {"/projects/my project", "my-project"},
		{"/projects/.config", ".config"}, {"/projects/中文", "中文"},
		{"/", "session"}, {"/projects/🔥", "session"}, {"/projects/a:b", "a-b"},
	} {
		base := directorySessionBase(tc.dir)
		if base != tc.want {
			t.Fatalf("base for %q = %q, want %q", tc.dir, base, tc.want)
		}
		for _, number := range []int{1, 2, 1000} {
			name := directorySessionCandidate(base, number)
			if err := protocol.ValidateSessionName(name); err != nil {
				t.Fatalf("invalid automatic name %q: %v", name, err)
			}
		}
	}
	for _, base := range []string{strings.Repeat("a", 200), strings.Repeat("界", 80)} {
		for _, number := range []int{1, 2, 1000} {
			name := directorySessionCandidate(base, number)
			if !utf8.ValidString(name) || protocol.ValidateSessionName(name) != nil {
				t.Fatalf("name truncation broke wire/UTF-8 limits: %q", name)
			}
			if number == 2 && !strings.HasSuffix(name, "-2") {
				t.Fatal("long name lost collision suffix")
			}
		}
	}
}

func fakeStartupRPC(t *testing.T, respond func(protocol.Request) protocol.Response) (string, <-chan protocol.Request) {
	t.Helper()
	dir, err := os.MkdirTemp("", "hv-start-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "rpc")
	l, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	done := make(chan struct{})
	requests := make(chan protocol.Request, 8)
	go func() {
		defer close(done)
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			if json.NewDecoder(c).Decode(&r) == nil {
				requests <- r
				response := respond(r)
				response.Version = protocol.Version
				_ = json.NewEncoder(c).Encode(response)
			}
			c.Close()
		}
	}()
	t.Cleanup(func() { l.Close(); <-done; os.RemoveAll(dir) })
	return socket, requests
}

func TestDirectoryStartupCreatesFreshSessionInRequestedDirectory(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing names", true: "concurrent collision"}[collision], func(t *testing.T) {
			socket, requests := fakeStartupRPC(t, func(r protocol.Request) protocol.Response {
				if r.Op == "list" {
					if collision {
						return protocol.Response{}
					}
					return protocol.Response{Sessions: []protocol.Session{{Name: "api"}, {Name: "api-2"}}}
				}
				if collision && r.Name == "api" {
					return protocol.Response{Error: "session already exists"}
				}
				return protocol.Response{}
			})
			cwd := "/projects/api"
			name, err := createDirectorySession(socket, cwd)
			want := "api-3"
			if collision {
				want = "api-2"
			}
			if err != nil || name != want {
				t.Fatalf("startup name = %q, error %v", name, err)
			}
			if r := <-requests; r.Op != "list" {
				t.Fatalf("unexpected lookup: %+v", r)
			}
			if collision {
				if r := <-requests; r.Op != "create" || r.Name != "api" {
					t.Fatalf("wrong collision request: %+v", r)
				}
			}
			if r := <-requests; r.Op != "create" || r.Name != want || r.Dir != cwd || r.Version != protocol.Version {
				t.Fatalf("startup didn't create a fresh session in cwd: %+v", r)
			}
		})
	}
}

func TestDirectoryStartupPropagatesErrorsWithoutRetryingUnrelatedFailures(t *testing.T) {
	for _, stage := range []string{"list", "create"} {
		t.Run(stage, func(t *testing.T) {
			socket, requests := fakeStartupRPC(t, func(r protocol.Request) protocol.Response {
				if r.Op == stage {
					return protocol.Response{Error: "server is shutting down"}
				}
				return protocol.Response{}
			})
			name, err := createDirectorySession(socket, "/projects/api")
			if name != "" || err == nil || !strings.Contains(err.Error(), "shutting down") {
				t.Fatalf("failure suppressed: %q %v", name, err)
			}
			_ = <-requests
			if stage == "create" {
				_ = <-requests
			}
			select {
			case r := <-requests:
				t.Fatalf("unrelated failure retried: %+v", r)
			default:
			}
		})
	}
}

func TestConcurrentDirectoryStartupCreatesDistinctPersistentSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("", "hv-auto-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "rpc")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	s := server.New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- s.Serve(l) }()
	defer func() {
		s.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	cwd := filepath.Join(t.TempDir(), "api")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	// An existing session with this name belongs to another directory and must
	// not be attached, overwritten, or terminated by automatic startup.
	other := t.TempDir()
	if _, err := client.Request(socket, protocol.Request{Op: "create", Name: "api", Dir: other}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		name string
		err  error
	}
	results := make(chan result, 4)
	var launches sync.WaitGroup
	for i := 0; i < 4; i++ {
		launches.Add(1)
		go func() {
			defer launches.Done()
			name, err := createDirectorySession(socket, cwd)
			results <- result{name, err}
		}()
	}
	launches.Wait()
	close(results)
	names := map[string]bool{}
	for r := range results {
		if r.err != nil || r.name == "api" || names[r.name] {
			t.Fatalf("startup reused or lost a session: %+v", r)
		}
		names[r.name] = true
	}
	r, err := client.Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sessions) != 5 {
		t.Fatalf("expected existing + 4 fresh sessions, got %d", len(r.Sessions))
	}
	for _, session := range r.Sessions {
		wantDir := cwd
		if session.Name == "api" {
			wantDir = other
		}
		if session.Dir != wantDir || session.Exited || session.Panes != 1 {
			t.Fatalf("incorrect persistent startup session: %+v", session)
		}
	}
}
