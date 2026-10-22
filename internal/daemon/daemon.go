// Package daemon starts and locates the per-user session server.
package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/server"
	"github.com/josephferrero/bmux/internal/update"
	"github.com/josephferrero/bmux/internal/version"
	"golang.org/x/sys/unix"
)

func Directory() (string, error) {
	if dir := os.Getenv("BMUX_RUNTIME_DIR"); dir != "" {
		return filepath.Abs(dir)
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "bmux"), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bmux"), nil
}

// StatePath is where sessions are saved for restarts and reboots. It lives
// outside the runtime directory, which is cleared at reboot on Linux, except
// that a custom BMUX_RUNTIME_DIR keeps its own sessions alongside it so
// separate servers never share or overwrite each other's state.
func StatePath() (string, error) {
	if dir := os.Getenv("BMUX_RUNTIME_DIR"); dir != "" {
		abs, err := filepath.Abs(dir)
		return filepath.Join(abs, "sessions.json"), err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "bmux", "sessions.json"), nil
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "bmux", "sessions.json"), nil
	}
	return filepath.Join(home, ".local", "state", "bmux", "sessions.json"), nil
}

// LayoutTrustPath is the store of trusted layout files (bmux layout trust),
// next to the saved sessions.
func LayoutTrustPath() (string, error) {
	state, err := StatePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(state), "trusted-layouts.json"), nil
}

func Prepare() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	stat, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("runtime path must be a private directory, not a symlink")
	}
	if owner, ok := stat.Sys().(*syscall.Stat_t); !ok || owner.Uid != uint32(os.Getuid()) {
		return "", fmt.Errorf("runtime directory is not owned by your user")
	}
	if stat.Mode().Perm() != 0700 {
		return "", fmt.Errorf("runtime directory %s must have permissions 0700", dir)
	}
	socket := filepath.Join(dir, "server.sock")
	// Darwin's sockaddr_un path limit is 104 bytes including its terminator.
	if len(socket) > 100 {
		return "", fmt.Errorf("runtime path is too long; set BMUX_RUNTIME_DIR to a shorter private directory")
	}
	return socket, nil
}

func Ensure(socket string) error {
	if _, err := protocol.Call(socket, protocol.Request{Op: "ping"}); err == nil {
		return nil
	}
	// If there is a listening server, don't start another even on a version mismatch.
	if c, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		c.Close()
		_, err = protocol.Call(socket, protocol.Request{Op: "ping"})
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(socket), "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(executable, "--serve")
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = protocol.Call(socket, protocol.Request{Op: "ping"}); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("server did not start: %w (see %s)", err, filepath.Join(filepath.Dir(socket), "server.log"))
}

// Stop asks the running server to save its sessions and exit, then waits for
// its socket to close. It reports whether a server was running and whether it
// could save: servers older than the shutdown request get SIGTERM instead and
// lose their sessions.
func Stop(socket string) (running, saved bool, err error) {
	c, dialErr := net.DialTimeout("unix", socket, time.Second)
	if dialErr != nil {
		return false, false, nil
	}
	c.Close()
	saved = true
	response, err := protocol.Call(socket, protocol.Request{Op: "shutdown"})
	var mismatch protocol.MismatchError
	switch {
	case err == nil:
	case errors.As(err, &mismatch) && response.Error == "":
		// Servers accept shutdown from any protocol version and save on the
		// way out; only the reply's version differs.
	case errors.As(err, &mismatch) || strings.Contains(err.Error(), "unknown operation"):
		// A server too old to take shutdown from this version: SIGTERM
		// stops it, saving too since v0.2.
		data, readErr := os.ReadFile(filepath.Join(filepath.Dir(socket), "server.pid"))
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if readErr != nil || parseErr != nil || pid <= 1 {
			return true, false, fmt.Errorf("this server can't be stopped from this bmux version and has no readable server.pid")
		}
		if err = syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return true, false, err
		}
		saved = false
	default:
		return true, false, err
	}
	// The socket closes first; the lock is released once the process exits,
	// after its panes are stopped. Only then can a new server start.
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(socket), "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return true, saved, err
	}
	defer lock.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil {
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
			return true, saved, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true, saved, fmt.Errorf("server did not stop within 10 seconds")
}

func Serve(socket string, stop <-chan os.Signal) error {
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(socket), "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("another bmux server owns this runtime directory: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if stat, err := os.Lstat(socket); err == nil {
		if stat.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket path %s", socket)
		}
		if err = os.Remove(socket); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer l.Close()
	defer os.Remove(socket)
	if err = os.Chmod(socket, 0600); err != nil {
		return err
	}
	pidFile := filepath.Join(filepath.Dir(socket), "server.pid")
	if err = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		return err
	}
	defer os.Remove(pidFile)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	s := server.New(shell)
	// Agent and restore settings come from the config file; problems are
	// logged and the affected settings keep their defaults.
	if path, err := config.Path(); err != nil {
		fmt.Fprintln(os.Stderr, "bmux: config:", err)
	} else if err = s.LoadSettings(path); err != nil {
		fmt.Fprintln(os.Stderr, "bmux: config:", err)
	}
	// Restore before accepting connections, so the first client already sees
	// the sessions. Without a usable state file the server still runs; the
	// reason is in server.log.
	if state, err := StatePath(); err != nil {
		fmt.Fprintln(os.Stderr, "bmux: sessions will not be saved:", err)
	} else if err = s.EnablePersistence(state); err != nil {
		fmt.Fprintln(os.Stderr, "bmux: sessions will not be saved:", err)
	}
	if trust, err := LayoutTrustPath(); err == nil {
		s.SetLayoutTrust(trust)
	}
	done := make(chan struct{})
	// Release builds look for newer releases once a day (update.check);
	// the cache sits next to the saved sessions so restarts don't re-ask.
	if current := version.String(); current != version.Dev {
		if state, err := StatePath(); err == nil {
			checker := update.Checker{Source: update.GitHub(), Cache: filepath.Join(filepath.Dir(state), "update-check.json"), Interval: update.CheckInterval, Now: time.Now}
			go s.WatchReleases(current, checker.Latest, time.Hour, done)
		}
	}
	// Like Helix, SIGUSR1 rereads the config file (pkill -USR1 -f 'bmux --serve').
	reload := make(chan os.Signal, 1)
	signal.Notify(reload, syscall.SIGUSR1)
	defer signal.Stop(reload)
	go func() {
		for {
			select {
			case <-stop:
				s.Close()
				return
			case <-reload:
				if err := s.ReloadSettings(); err != nil {
					fmt.Fprintln(os.Stderr, "bmux: config:", err)
				}
			case <-done:
				return
			}
		}
	}()
	err = s.Serve(l)
	close(done)
	s.Close()
	return err
}
