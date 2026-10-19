package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"text/tabwriter"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/daemon"
	"github.com/josephferrero/bmux/internal/protocol"
)

const version = "0.1.1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bmux:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-h":
			help()
			return nil
		case "version", "--version":
			fmt.Println("bmux", version)
			return nil
		}
	}
	socket, err := daemon.Prepare()
	if err != nil {
		return err
	}
	if len(args) == 1 && args[0] == "--serve" {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
		defer signal.Stop(stop)
		return daemon.Serve(socket, stop)
	}
	if len(args) > 0 && args[0] != "new" && args[0] != "attach" && args[0] != "list" && args[0] != "rename" && args[0] != "kill" && args[0] != "focus" {
		return fmt.Errorf("unknown command %q; try bmux --help", args[0])
	}
	if err = daemon.Ensure(socket); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		name, err := createDirectorySession(socket, cwd)
		if err != nil {
			return err
		}
		return client.Run(socket, name, cwd)
	}
	switch args[0] {
	case "new":
		flags := flag.NewFlagSet("new", flag.ContinueOnError)
		dir := flags.String("dir", cwd, "working directory")
		detached := flags.Bool("detach", false, "create without attaching")
		if err := flags.Parse(args[1:]); err != nil {
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
	case "attach":
		name, err := attachTarget(socket, args[1:])
		if err != nil {
			return err
		}
		return client.Run(socket, name, cwd)
	case "list":
		if len(args) != 1 {
			return fmt.Errorf("usage: bmux list")
		}
		r, err := client.Request(socket, protocol.Request{Op: "list"})
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SESSION\tSTATE\tPANES\tCLIENTS\tDIRECTORY")
		for _, s := range r.Sessions {
			state := "running"
			if s.Exited {
				state = "exited"
			}
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", s.Name, state, s.Panes, s.Clients, s.Dir)
		}
		return w.Flush()
	case "rename":
		if len(args) != 3 {
			return fmt.Errorf("usage: bmux rename OLD NEW")
		}
		_, err = client.Request(socket, protocol.Request{Op: "rename", Name: args[1], NewName: args[2]})
		return err
	case "kill":
		if len(args) != 2 {
			return fmt.Errorf("usage: bmux kill NAME (stops all its panes)")
		}
		_, err = client.Request(socket, protocol.Request{Op: "kill", Name: args[1]})
		return err
	case "focus":
		// For editors handing focus back to bmux at an edge. Called inside a
		// pane, so the session and pane come from the environment, like tmux's
		// TMUX_PANE. Deliberately not Ensure(): a missing server is an error,
		// not something to start.
		if len(args) != 2 {
			return fmt.Errorf("usage: bmux focus left|right|up|down")
		}
		session := os.Getenv("BMUX_SESSION")
		if session == "" {
			return fmt.Errorf("not inside a bmux pane")
		}
		// The ID survives renames. Servers older than v0.1.1 ignore it and use the name.
		sessionID, _ := strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
		paneID, _ := strconv.Atoi(os.Getenv("BMUX_PANE"))
		_, err := client.Request(socket, protocol.Request{Op: "focus-pane", Name: session, SessionID: sessionID, PaneID: paneID, Direction: args[1]})
		return err
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

func help() {
	fmt.Println(helpText)
}

const helpText = `bmux — persistent workspaces, ordinary terminal interaction.

Usage:
  bmux                                   Create and attach a fresh session in this directory
  bmux new [--dir PATH] [--detach] NAME  Create a shell session
  bmux attach [NAME]                     Attach by name, or to the first listed session
  bmux list                              List sessions
  bmux rename OLD NEW                    Rename a session
  bmux kill NAME                         Terminate a session and all its panes
  bmux focus left|right|up|down          Move pane focus (for editors at an edge)
  bmux version                           Print the version

Inside a session:
  Ctrl-a s   Session picker       Ctrl-a d   Detach (shell keeps running)
  Ctrl-a n   New session          Ctrl-a p   Last session
  Ctrl-a y   Copy selection       Ctrl-a Ctrl-a   Send Ctrl-a to application
  Ctrl-a |   Side-by-side split   Ctrl-a -   Stacked split
  Ctrl-a h/j/k/l   Resize divider by five cells
                  Repeat plain h/j/k/l until a one-second pause
  Ctrl-h/j/k/l    Navigate bmux panes, or forward to foreground Neovim/Vim
  Ctrl-a arrows   Focus pane      Ctrl-a o   Cycle visible panes
  Ctrl-a m   Zoom / unzoom        Click a pane to focus it

Session picker:
  j/k or arrows   Select          Enter   Attach
  n   New session                d       Delete session (confirmation required)
  r   Rename                     /       Optional search
  The highlighted session's live terminal is shown below the list, read-only.
  Native text, source colors, and your terminal's font; no image protocols.

Drag to select and copy. Double-click words; triple-click lines.
Mouse wheel scrolls history. Paste using your terminal's normal shortcut.
Hold Alt to select instead of sending mouse events to a mouse-aware app.

This is an early prototype. Sessions survive detach, not server exit or reboot.`
