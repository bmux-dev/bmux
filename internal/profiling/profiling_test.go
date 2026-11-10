package profiling

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func privateDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bmux-pprof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func socketPath(dir, role string) string {
	return filepath.Join(dir, fmt.Sprintf("%s-%d.sock", role, os.Getpid()))
}

func profileClient(t *testing.T, socket string) *http.Client {
	t.Helper()
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func TestDisabled(t *testing.T) {
	stop, err := Start("", "client")
	if err != nil {
		t.Fatal(err)
	}
	stop()
}

func TestProfilesAndCleanup(t *testing.T) {
	dir := privateDirectory(t)
	stop, err := Start(dir, "client")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	socket := socketPath(dir, "client")
	info, err := os.Stat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("profile socket permissions: %v, %v", info, err)
	}
	client := profileClient(t, socket)
	for _, path := range []string{"allocs", "heap", "profile?seconds=1"} {
		t.Run(path, func(t *testing.T) {
			response, err := client.Get("http://localhost/debug/pprof/" + path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("profile status %d: %s", response.StatusCode, body)
			}
			compressed, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			reader, err := gzip.NewReader(bytes.NewReader(compressed))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			profile, err := io.ReadAll(reader)
			if err != nil || len(profile) == 0 {
				t.Fatalf("empty or unreadable profile: %v", err)
			}
		})
	}
	for _, path := range []string{"cmdline", "trace", "goroutine"} {
		response, err := client.Get("http://localhost/debug/pprof/" + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("unexpected exposed endpoint %s: %d", path, response.StatusCode)
		}
	}
	response, err := client.Post("http://localhost/debug/pprof/heap", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST profile status = %d", response.StatusCode)
	}
	stop()
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("socket remained after cleanup: %v", err)
	}
	if _, err := client.Get("http://localhost/debug/pprof/heap"); err == nil {
		t.Fatal("profile listener still answers after cleanup")
	}
}

func TestClientAndServerUseSeparateSockets(t *testing.T) {
	dir := privateDirectory(t)
	for _, role := range []string{"client", "server"} {
		stop, err := Start(dir, role)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(stop)
		if _, err := os.Stat(socketPath(dir, role)); err != nil {
			t.Fatal(err)
		}
	}
	if stop, err := Start(dir, "client"); err == nil {
		stop()
		t.Fatal("a duplicate listener replaced the existing socket")
	}
}

func TestRepeatedCleanupDoesNotRemoveReplacementListener(t *testing.T) {
	dir := privateDirectory(t)
	stop, err := Start(dir, "client")
	if err != nil {
		t.Fatal(err)
	}
	stop()
	replacement, err := Start(dir, "client")
	if err != nil {
		t.Fatal(err)
	}
	defer replacement()
	stop()
	response, err := profileClient(t, socketPath(dir, "client")).Get("http://localhost/debug/pprof/heap")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("replacement listener status = %d", response.StatusCode)
	}
}

func TestRejectUnsafeDirectories(t *testing.T) {
	t.Run("permissions", func(t *testing.T) {
		dir := privateDirectory(t)
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if stop, err := Start(dir, "server"); err == nil {
			stop()
			t.Fatal("non-private directory accepted")
		}
		info, _ := os.Stat(dir)
		if info.Mode().Perm() != 0755 {
			t.Fatal("changed an existing directory's permissions")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := privateDirectory(t)
		link := filepath.Join(dir, "link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		if stop, err := Start(link, "server"); err == nil {
			stop()
			t.Fatal("symlink directory accepted")
		}
	})
	t.Run("file", func(t *testing.T) {
		path := filepath.Join(privateDirectory(t), "file")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if stop, err := Start(path, "server"); err == nil {
			stop()
			t.Fatal("file accepted as profile directory")
		}
	})
	t.Run("path length", func(t *testing.T) {
		if stop, err := Start(filepath.Join(privateDirectory(t), strings.Repeat("x", 101)), "server"); err == nil {
			stop()
			t.Fatal("overlong socket path accepted")
		}
	})
	t.Run("role", func(t *testing.T) {
		if stop, err := Start(privateDirectory(t), "../other"); err == nil {
			stop()
			t.Fatal("invalid role accepted")
		}
	})
}

func TestCreatesPrivateDirectory(t *testing.T) {
	dir := filepath.Join(privateDirectory(t), "new")
	stop, err := Start(dir, "server")
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("profile directory permissions: %v, %v", info, err)
	}
}
