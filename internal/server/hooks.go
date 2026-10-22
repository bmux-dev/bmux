package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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

// runHook runs one hook and logs its output and failure.
func runHook(event, command string, info protocol.PaneInfo) {
	output, err := runCommand(command, info, "BMUX_HOOK="+event)
	if len(output) > 0 {
		fmt.Fprintf(os.Stderr, "bmux: hook %s: %s", event, output)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bmux: hook %s %q: %v\n", event, command, err)
	}
}

// runCommand runs command with /bin/sh for the pane info describes: its
// #{...} variables, its directory and its BMUX_* environment (so bmux
// commands in it act on that pane), plus extra variables. It waits, killing
// the command's process group after hookTimeout.
func runCommand(command string, info protocol.PaneInfo, extra ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hookTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", format.Expand(command, format.Vars(info)))
	cmd.Dir = hookDir(info)
	cmd.Env = append(shellEnv(os.Environ(), info.SessionID, info.Session, info.Pane), extra...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	return cmd.CombinedOutput()
}

// runShell runs a key's run-shell command for pane id (0: the focused
// pane) in the background, then shows the last line it printed, or why it
// failed, in the status line of the session's clients.
func (s *session) runShell(id int, command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("run-shell needs a command")
	}
	select {
	case hookSlots <- struct{}{}:
	default:
		s.showMessage(fmt.Sprintf("run-shell skipped: %d commands are already running", maxHooks))
		return nil
	}
	go func() {
		defer func() { <-hookSlots }()
		panes, err := s.describe(paneTarget{pane: id}, "pane")
		if err != nil || len(panes) != 1 {
			s.showMessage("run-shell: pane not found")
			return
		}
		output, err := runCommand(command, panes[0], "BMUX_RUN_SHELL=1")
		last := lastLine(output)
		switch {
		case err != nil && last != "":
			s.showMessage(fmt.Sprintf("run-shell failed (%v): %s", err, last))
		case err != nil:
			s.showMessage(fmt.Sprintf("run-shell failed: %v", err))
		case last != "":
			s.showMessage(last)
		}
	}()
	return nil
}

// lastLine is output's last non-blank line, cut to a status line's width.
func lastLine(output []byte) string {
	lines := strings.Split(strings.TrimRight(string(output), "\n\r\t "), "\n")
	line := strings.Join(strings.Fields(lines[len(lines)-1]), " ")
	if r := []rune(line); len(r) > 200 {
		line = string(r[:200]) + "…"
	}
	return line
}

// showMessage puts text in the status line of every client attached to
// the session, like bmux display-message.
func (s *session) showMessage(text string) {
	s.mu.Lock()
	s.message = text
	s.messageSeq++
	s.revision++
	s.mu.Unlock()
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
