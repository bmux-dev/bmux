package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/daemon"
	"github.com/josephferrero/bmux/internal/protocol"
)

const version = "0.3.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bmux:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "--serve" {
		socket, err := daemon.Prepare()
		if err != nil {
			return err
		}
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
		defer signal.Stop(stop)
		return daemon.Serve(socket, stop)
	}
	name, rest := "", args
	if len(args) > 0 {
		name, rest = args[0], args[1:]
	}
	switch name {
	case "--help", "-h":
		name = "help"
	case "--version":
		name = "version"
	}
	c, ok := findCommand(name)
	if !ok {
		return fmt.Errorf("unknown command %q; try bmux --help", name)
	}
	if c.server == noSocket {
		return c.run("", rest)
	}
	socket, err := daemon.Prepare()
	if err != nil {
		return err
	}
	if c.server == startServer {
		if startsClient(args) {
			if err := refuseNesting(); err != nil {
				return err
			}
		}
		if err := daemon.Ensure(socket); err != nil {
			return err
		}
	}
	return c.run(socket, rest)
}

func runDefault(socket string, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	name, err := createDirectorySession(socket, cwd)
	if err != nil {
		return err
	}
	return client.Run(socket, name, cwd)
}

func runNew(socket string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("new", flag.ContinueOnError)
	dir := flags.String("dir", cwd, "working directory")
	detached := flags.Bool("detach", false, "create without attaching")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: bmux new [--dir PATH] [--detach] NAME")
	}
	absolute, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	name := flags.Arg(0)
	if _, err = client.Request(socket, protocol.Request{Op: "create", Name: name, Dir: absolute}); err != nil {
		return err
	}
	if *detached {
		fmt.Println("Created session", name)
		return nil
	}
	return client.Run(socket, name, cwd)
}

func runAttach(socket string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	name, err := attachTarget(socket, args)
	if err != nil {
		return err
	}
	return client.Run(socket, name, cwd)
}

func runList(socket string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bmux list")
	}
	r, err := client.Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tSTATE\tWINDOWS\tPANES\tCLIENTS\tDIRECTORY")
	for _, s := range r.Sessions {
		state := "running"
		if s.Exited {
			state = "exited"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\n", s.Name, state, s.Windows, s.Panes, s.Clients, s.Dir)
	}
	return w.Flush()
}

func runRename(socket string, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: bmux rename OLD NEW")
	}
	_, err := client.Request(socket, protocol.Request{Op: "rename", Name: args[0], NewName: args[1]})
	return err
}

func runKill(socket string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bmux kill NAME (stops all its panes)")
	}
	_, err := client.Request(socket, protocol.Request{Op: "kill", Name: args[0]})
	return err
}

// runFocus is for editors handing focus back to bmux at an edge. Called
// inside a pane, so the session and pane come from the environment, like
// tmux's TMUX_PANE.
func runFocus(socket string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bmux focus left|right|up|down")
	}
	session := os.Getenv("BMUX_SESSION")
	if session == "" {
		return fmt.Errorf("not inside a bmux pane")
	}
	// The ID survives renames. Servers older than v0.1.1 ignore it and use the name.
	sessionID, _ := strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
	paneID, _ := strconv.Atoi(os.Getenv("BMUX_PANE"))
	_, err := client.Request(socket, protocol.Request{Op: "focus-pane", Name: session, SessionID: sessionID, PaneID: paneID, Direction: args[0]})
	return err
}

func runRestart(socket string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bmux restart")
	}
	return restartServer(socket)
}

// runReload rereads the config file in the server; attached clients follow.
func runReload(socket string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bmux reload")
	}
	path, _ := config.Path()
	if _, err := client.Request(socket, protocol.Request{Op: "reload-config"}); err != nil {
		return err
	}
	fmt.Println("Reloaded", path)
	return nil
}

// startsClient reports whether a command would attach this terminal.
func startsClient(args []string) bool {
	if len(args) == 0 || args[0] == "attach" {
		return true
	}
	if args[0] != "new" {
		return false
	}
	for _, arg := range args[1:] {
		switch arg {
		case "-detach", "--detach", "-detach=true", "--detach=true":
			return false
		case "--":
			return true
		}
	}
	return true
}

// refuseNesting stops a client from starting inside a bmux pane, like tmux
// checks $TMUX: a nested client shrinks into the pane and fights over keys.
func refuseNesting() error {
	if session := os.Getenv("BMUX_SESSION"); session != "" {
		return fmt.Errorf("already inside bmux session %s; switch with Ctrl-a s, create with bmux new --detach, or unset BMUX_SESSION to nest anyway", session)
	}
	return nil
}

func reportAgent(socket string, args []string) error {
	// Everything after -- is the command that resumes this conversation.
	var resume []string
	for i, arg := range args {
		if arg == "--" {
			args, resume = args[:i], args[i+1:]
			break
		}
	}
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: bmux agent working|blocked|idle|clear [NAME] [-- RESUME COMMAND...]")
	}
	label := ""
	if len(args) == 2 {
		label = args[1]
	}
	return sendAgentReport(socket, args[0], label, resume)
}

// sendAgentReport reports for the pane this process runs in, identified by
// the environment bmux gives every pane.
func sendAgentReport(socket, state, label string, resume []string) error {
	session := os.Getenv("BMUX_SESSION")
	if session == "" {
		return fmt.Errorf("not inside a bmux pane")
	}
	sessionID, _ := strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
	paneID, _ := strconv.Atoi(os.Getenv("BMUX_PANE"))
	_, err := client.Request(socket, protocol.Request{Op: "agent-report", Name: session, SessionID: sessionID, PaneID: paneID, Kind: state, Text: label, Args: resume})
	return err
}

// restartServer saves and stops the server, then starts this binary's server,
// which restores the saved sessions. Attached clients reconnect on their own.
func restartServer(socket string) error {
	if os.Getenv("BMUX_SESSION") != "" {
		// Stopping the server ends this pane's jobs, including this command,
		// so finish the restart from a process outside the pane's session.
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(executable, "restart")
		// A pane's environment holds whatever its programs added, such as an
		// agent's marker that it spawned this shell, and every new pane would
		// inherit it. The new server keeps the running server's environment;
		// only servers that predate the request get this pane's.
		cmd.Env = append(os.Environ(), "BMUX_SESSION=")
		if r, err := client.Request(socket, protocol.Request{Op: "environment"}); err == nil && len(r.Env) > 0 {
			cmd.Env = r.Env
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return err
		}
		_ = cmd.Process.Release()
		fmt.Println("Restarting the bmux server; attached clients reconnect when it's back")
		return nil
	}
	running, saved, err := daemon.Stop(socket)
	if err != nil {
		return err
	}
	if err := daemon.Ensure(socket); err != nil {
		return err
	}
	r, err := client.Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return err
	}
	switch {
	case running && !saved:
		fmt.Printf("Restarted the bmux server; %d sessions restored from the last save. The previous server predated saving, so changes since then were lost.\n", len(r.Sessions))
	case running:
		fmt.Printf("Restarted the bmux server; %d sessions restored\n", len(r.Sessions))
	default:
		fmt.Printf("Started the bmux server; %d sessions restored\n", len(r.Sessions))
	}
	if len(r.Sessions) > 0 {
		fmt.Println("Return to them with bmux attach [NAME]; bare bmux creates another session")
	}
	return nil
}

func attachTarget(socket string, names []string) (string, error) {
	if len(names) > 1 {
		return "", fmt.Errorf("usage: bmux attach [NAME]")
	}
	if len(names) == 1 {
		return names[0], nil
	}
	r, err := client.Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return "", err
	}
	if len(r.Sessions) == 0 {
		return "", fmt.Errorf("no sessions available; create one with bmux new NAME")
	}
	return r.Sessions[0].Name, nil
}

// helpText is the usage lines from the command table, then interactive help.
func helpText() string {
	var b strings.Builder
	b.WriteString("bmux — persistent workspaces, ordinary terminal interaction.\n\n")
	b.WriteString("Agents: run bmux guide to learn how to read, drive and watch panes.\n\n")
	b.WriteString("Usage:\n")
	for _, c := range commands {
		line := strings.TrimSpace("bmux " + c.name + " " + c.usage)
		const column = 37
		if len(line) > column {
			fmt.Fprintf(&b, "  %s\n  %-*s  %s\n", line, column, "", c.summary)
		} else {
			fmt.Fprintf(&b, "  %-*s  %s\n", column, line, c.summary)
		}
	}
	b.WriteString(interactiveHelp)
	return b.String()
}

const interactiveHelp = `
Inside a session:
  Ctrl-a s   Session picker       Ctrl-a d   Detach (shell keeps running)
  Ctrl-a n   New session          Ctrl-a p   Last session
  Ctrl-a ( / )   Previous / next session
  Ctrl-a c   New window           Ctrl-a 1-9   Select window
  Ctrl-a [ / ]   Previous / next window      Ctrl-a Tab   Last window
  Ctrl-a ,   Rename window        Ctrl-a &   Close window
  Ctrl-a y   Copy selection       Ctrl-a Ctrl-a   Send Ctrl-a to application
  Ctrl-a |   Side-by-side split   Ctrl-a -   Stacked split
  Ctrl-a ;   Last pane            Ctrl-a { / }   Swap with previous / next pane
  Ctrl-a Ctrl-o   Rotate panes    Ctrl-a ! / @   Pane to new window / into last window
  Ctrl-a Space   Next layout (Alt-1..5 pick one)   Ctrl-a E   Even out splits
  Ctrl-a h/j/k/l   Resize divider by five cells
                  Repeat plain h/j/k/l until a one-second pause
  Ctrl-h/j/k/l    Navigate bmux panes, or forward to foreground Neovim/Vim
  Ctrl-a arrows   Focus pane      Ctrl-a o   Cycle visible panes
  Ctrl-a m   Zoom / unzoom        Click a pane to focus it
  Ctrl-a x   Close pane (asks first if a program is running)
  Ctrl-a b   Show / hide sidebar  Ctrl-a r   Reload config
  Ctrl-a ?   List key bindings (rebind them in the config file)
  Ctrl-a i   Agent integrations: exact agent state, installed on Enter

Settings: ~/.config/bmux/config.toml (see bmux default-config).

Sidebar (Ctrl-h from the leftmost pane):
  j/k   Select session or agent  Enter/l   Open it
  Esc   Back to panes            n         New session
  d     Delete session (confirmation required)
  ● working (yellow) · ● waiting or rang while unseen (teal) · ● blocked (red) · ○ idle (green)
  A sound plays when a background agent becomes ready or blocked (BMUX_SOUND=0 mutes)

Session picker:
  j/k or arrows   Select          Enter   Attach
  n   New session                d       Delete session (confirmation required)
  r   Rename                     /       Optional search
  The highlighted session's live terminal is shown below the list, read-only.
  Native text, source colors, and your terminal's font; no image protocols.

Drag to select and copy. Double-click words; triple-click lines.
Mouse wheel scrolls history. Paste using your terminal's normal shortcut.
Hold Alt to select instead of sending mouse events to a mouse-aware app.

Sessions survive detach, restarts and reboots as layouts and directories with
fresh shells; recognized agents reopen their conversations. Use bmux restart
to upgrade the server.`
