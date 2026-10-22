package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/format"
	"github.com/josephferrero/bmux/internal/protocol"
)

// run-shell and if-shell run shell commands for a pane, like tmux's: its
// #{...} variables, its directory and its BMUX_* environment, so bmux
// commands in them act on that pane. They take -t TARGET (the calling pane
// by default) because if-shell's words are all commands.

// shellPane describes the pane -t names, or the calling pane.
func shellPane(socket, target string) (protocol.PaneInfo, error) {
	r, err := describeRequest("pane", target)
	if err != nil {
		return protocol.PaneInfo{}, err
	}
	panes, err := describe(socket, r)
	if err != nil {
		return protocol.PaneInfo{}, err
	}
	if len(panes) != 1 {
		return protocol.PaneInfo{}, fmt.Errorf("pane not found")
	}
	return panes[0], nil
}

// paneShell is /bin/sh running command for info's pane, attached to this
// terminal.
func paneShell(info protocol.PaneInfo, command string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-c", format.Expand(command, format.Vars(info)))
	for _, dir := range []string{info.Path, info.SessionPath} {
		if stat, err := os.Stat(dir); dir != "" && err == nil && stat.IsDir() {
			cmd.Dir = dir
			break
		}
	}
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "BMUX_SESSION" && key != "BMUX_SESSION_ID" && key != "BMUX_PANE" {
			env = append(env, entry)
		}
	}
	cmd.Env = append(env, "BMUX_SESSION="+info.Session, "BMUX_SESSION_ID="+strconv.FormatUint(info.SessionID, 10), "BMUX_PANE="+strconv.Itoa(info.Pane))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

// exitStatus passes a command's failure on as bmux's: its exit code, and no
// extra message for an ordinary non-zero exit.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func commandError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exitStatus(exit.ExitCode())
	}
	return err
}

func runRunShell(socket string, args []string) error {
	const usage = "usage: bmux run-shell [-t TARGET] [-b] COMMAND"
	a, err := parseLayoutArgs(args, usage, []string{"-t"}, []string{"-b"})
	if err != nil {
		return err
	}
	if len(a.words) != 1 {
		return fmt.Errorf("%s (quote COMMAND)", usage)
	}
	info, err := shellPane(socket, a.values["-t"])
	if err != nil {
		return err
	}
	cmd := paneShell(info, a.words[0])
	if a.flags["-b"] {
		// In the background, in its own session, so it outlives this one.
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	return commandError(cmd.Run())
}

func runIfShell(socket string, args []string) error {
	const usage = "usage: bmux if-shell [-t TARGET] CONDITION COMMAND [ELSE]"
	a, err := parseLayoutArgs(args, usage, []string{"-t"}, nil)
	if err != nil {
		return err
	}
	if len(a.words) < 2 || len(a.words) > 3 {
		return fmt.Errorf("%s (quote each one)", usage)
	}
	info, err := shellPane(socket, a.values["-t"])
	if err != nil {
		return err
	}
	condition := paneShell(info, a.words[0])
	branch := a.words[1]
	if err := condition.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return err // the condition couldn't run at all
		}
		if len(a.words) < 3 {
			return nil
		}
		branch = a.words[2]
	}
	return commandError(paneShell(info, branch).Run())
}

func runPipePane(socket string, args []string) error {
	const usage = "usage: bmux pipe-pane [-t TARGET] [-o] [COMMAND]"
	a, err := parseLayoutArgs(args, usage, []string{"-t"}, []string{"-o"})
	if err != nil {
		return err
	}
	if len(a.words) > 1 {
		return fmt.Errorf("%s (quote COMMAND)", usage)
	}
	r, err := layoutRequest("", a.values["-t"])
	if err != nil {
		return err
	}
	r.Op, r.Kind = "pipe-pane", ""
	if a.flags["-o"] {
		r.Kind = "toggle"
	}
	if len(a.words) == 1 {
		if strings.TrimSpace(a.words[0]) == "" {
			return fmt.Errorf("COMMAND is empty; leave it out to stop piping")
		}
		r.Text = a.words[0]
	}
	_, err = client.Request(socket, r)
	return err
}
