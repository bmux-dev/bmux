package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/josephferrero/bmux/internal/format"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Hooks are shell commands from the config file's [hooks] table that the
// server runs when an event happens, like tmux's set-hook. They run in the
// background, never under a state lock, each in its own process group with
// a time limit, and at most maxHooks at once.

const maxHooks = 8

// hookTimeout is how long one hook may run; tests shorten it.
var hookTimeout = time.Minute

var hookSlots = make(chan struct{}, maxHooks)

// runHooks starts every command configured for event, expanding #{...}
// with info, the event's pane. It returns at once.
func runHooks(event string, info protocol.PaneInfo) {
	for _, command := range currentSettings().hooks[event] {
		select {
		case hookSlots <- struct{}{}:
		default:
			fmt.Fprintf(os.Stderr, "bmux: hook %s skipped: %d hooks are already running\n", event, maxHooks)
			continue
		}
		go func() {
			defer func() { <-hookSlots }()
			runHook(event, command, info)
		}()
	}
}

// runHook runs one command and waits for it, killing its process group
// after hookTimeout. Output and failures go to the server log.
func runHook(event, command string, info protocol.PaneInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", format.Expand(command, format.Vars(info)))
	cmd.Dir = hookDir(info)
	// The pane's environment, so bmux commands in the hook act on its pane.
	cmd.Env = append(shellEnv(os.Environ(), info.SessionID, info.Session, info.Pane), "BMUX_HOOK="+event)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		fmt.Fprintf(os.Stderr, "bmux: hook %s: %s", event, output)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmux: hook %s %q: %v\n", event, command, err)
	}
}

// hookDir is where a hook runs: the pane's directory, else its session's,
// else the home directory.
func hookDir(info protocol.PaneInfo) string {
	for _, dir := range []string{info.Path, info.SessionPath} {
		if stat, err := os.Stat(dir); dir != "" && err == nil && stat.IsDir() {
			return dir
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// hook runs event's hooks for pane id once session.mu is free; it never
// waits on the hooks. Callers may hold session.mu.
func (s *session) hook(event string, id int) {
	if len(currentSettings().hooks[event]) == 0 {
		return
	}
	go func() {
		panes, err := s.describe(paneTarget{pane: id}, "pane")
		if err == nil && len(panes) == 1 {
			runHooks(event, panes[0])
		}
	}()
}

// goneInfo describes a pane that has left, from what the session still
// knows: there's no directory or program to read any more. Caller holds
// session.mu.
func (s *session) goneInfo(id, window int) protocol.PaneInfo {
	return protocol.PaneInfo{
		Session: s.info.Name, SessionID: s.id, SessionPath: s.info.Dir, SessionWindows: len(s.windows),
		SessionAttached: s.info.Clients, Window: window, Pane: id,
	}
}
