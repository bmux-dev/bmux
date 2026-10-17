// Package daemon starts and locates the per-user session server.
package daemon

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/server"
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
	done := make(chan struct{})
	go func() {
		select {
		case <-stop:
			s.Close()
		case <-done:
		}
	}()
	err = s.Serve(l)
	close(done)
	s.Close()
	return err
}
