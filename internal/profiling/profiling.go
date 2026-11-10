// Package profiling exposes opt-in Go profiles over private Unix sockets.
package profiling

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Start returns a cleanup function for this process's profiling listener. An
// empty directory disables profiling entirely. Existing directories must be
// private and owned by the user; do not change their permissions or replace
// an existing socket.
func Start(directory, role string) (func(), error) {
	if directory == "" {
		return func() {}, nil
	}
	if role != "client" && role != "server" {
		return nil, fmt.Errorf("profiling: unknown process role %q", role)
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("profiling directory: %w", err)
	}
	socket := filepath.Join(directory, fmt.Sprintf("%s-%d.sock", role, os.Getpid()))
	// Keep the path portable to Darwin's 104-byte sockaddr_un, including NUL.
	if len(socket) > 100 {
		return nil, fmt.Errorf("profiling socket path is too long; use a shorter BMUX_PPROF_DIR")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("profiling directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, fmt.Errorf("profiling directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("BMUX_PPROF_DIR must be a private directory, not a symlink")
	}
	if owner, ok := info.Sys().(*syscall.Stat_t); !ok || owner.Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("BMUX_PPROF_DIR must be owned by your user")
	}
	if info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("BMUX_PPROF_DIR must have permissions 0700")
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("profiling socket: %w", err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("profiling socket permissions: %w", err)
	}
	// Do not serve the default mux: importing pprof registers global handlers,
	// but this listener intentionally exposes only the profiles we need.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.Handle("GET /debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("GET /debug/pprof/heap", pprof.Handler("heap"))
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	var closed sync.Once
	return func() {
		closed.Do(func() {
			_ = server.Close()
			// Also close explicitly if cleanup raced Serve's initialization.
			// Closing the Unix listener removes its socket, not the directory.
			_ = listener.Close()
			<-done
		})
	}, nil
}
