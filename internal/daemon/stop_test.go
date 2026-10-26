package daemon

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// fakeServer answers every request on dir's socket with reply, as a running
// server of another release would, and records the ops it was sent.
func fakeServer(t *testing.T, dir string, reply protocol.Response) (socket string, ops chan string) {
	t.Helper()
	socket = filepath.Join(dir, "server.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	ops = make(chan string, 8)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			var r protocol.Request
			if json.NewDecoder(c).Decode(&r) == nil {
				ops <- r.Op
				_ = json.NewEncoder(c).Encode(reply)
			}
			c.Close()
		}
	}()
	return socket, ops
}

func shortDir(t *testing.T) string {
	t.Helper()
	// A short path avoids sockaddr_un limits on macOS.
	dir, err := os.MkdirTemp("", "bm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestStopCountsAnAcceptedShutdownFromAnotherVersionAsSaved(t *testing.T) {
	// An older server takes shutdown from any version and saves on the way
	// out; only its reply's protocol version differs.
	socket, ops := fakeServer(t, shortDir(t), protocol.Response{Version: protocol.Version - 2})
	running, saved, err := Stop(socket)
	if err != nil || !running || !saved {
		t.Fatalf("Stop = running %v, saved %v, %v; want a saved stop", running, saved, err)
	}
	if op := <-ops; op != "shutdown" {
		t.Fatalf("sent %q, want shutdown", op)
	}
}

func TestStopFallsBackToSIGTERMForAServerThatRefuses(t *testing.T) {
	dir := shortDir(t)
	socket, _ := fakeServer(t, dir, protocol.Response{Version: protocol.Version - 2, Error: "protocol version mismatch; use the same bmux binary for client and server"})
	// server.pid names a stand-in process for the server to stop.
	stand := exec.Command("sleep", "30")
	if err := stand.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stand.Process.Kill() })
	if err := os.WriteFile(filepath.Join(dir, "server.pid"), []byte(strconv.Itoa(stand.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	running, saved, err := Stop(socket)
	if err != nil || !running || saved {
		t.Fatalf("Stop = running %v, saved %v, %v; want a SIGTERM stop", running, saved, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- stand.Wait() }()
	select {
	case err := <-exited:
		if err == nil || !strings.Contains(err.Error(), "terminated") {
			t.Fatalf("stand-in exited with %v, want SIGTERM", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the refusing server wasn't sent SIGTERM")
	}
}

func TestCommandsAgainstAnotherVersionSayWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		server int
		want   string
	}{
		{protocol.Version - 2, "bmux restart"},             // after bmux update
		{protocol.Version + 1, "the bmux that started it"}, // an older binary than the server
	} {
		socket, _ := fakeServer(t, shortDir(t), protocol.Response{Version: tc.server})
		_, err := protocol.Call(socket, protocol.Request{Op: "list"})
		var mismatch protocol.MismatchError
		if !errors.As(err, &mismatch) || mismatch.Server != tc.server || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("server protocol %d: %v, want a mismatch naming %d that says %q", tc.server, err, tc.server, tc.want)
		}
	}
}
