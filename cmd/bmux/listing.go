package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/format"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Listing commands describe panes and windows, like tmux's list-panes,
// list-windows and display-message, with #{...} format variables.

// describeRequest asks for the panes in scope around targetText, or around
// the calling pane when it's empty.
func describeRequest(scope, targetText string) (protocol.Request, error) {
	r, err := layoutRequest(scope, targetText)
	r.Op, r.Kind = "describe", scope
	return r, err
}

// describe sends r and returns the panes it names.
func describe(socket string, r protocol.Request) ([]protocol.PaneInfo, error) {
	response, err := client.Request(socket, r)
	if err != nil {
		return nil, err
	}
	return response.Panes, nil
}

// listScope turns list-panes' and list-windows' target and -a into a
// describe request. A pane or window target, or none, gets scope narrow; a
// session target gets the whole session.
func listScope(a layoutArgs, usage, narrow string) (protocol.Request, error) {
	target, err := oneTarget(a.words, usage)
	if err != nil {
		return protocol.Request{}, err
	}
	if a.flags["-a"] {
		if target != "" {
			return protocol.Request{}, fmt.Errorf("-a lists every session; it takes no TARGET")
		}
		return protocol.Request{Op: "describe", Kind: "all"}, nil
	}
	if target == "" && os.Getenv("BMUX_SESSION") == "" {
		return protocol.Request{}, fmt.Errorf("not inside a bmux pane; name a TARGET or use -a for every session")
	}
	scope := narrow
	if target != "" {
		t, err := parseTarget(target)
		if err != nil {
			return protocol.Request{}, err
		}
		if t.Window == 0 && !t.names() {
			scope = "session"
		}
	}
	return describeRequest(scope, target)
}

func runListPanes(socket string, args []string) error {
	const usage = "usage: bmux list-panes [TARGET] [-a] [-F FORMAT | --json]"
	a, err := parseLayoutArgs(args, usage, []string{"-F"}, []string{"-a", "--json"})
	if err != nil {
		return err
	}
	// A pane or window target lists its window; a session, every window's
	// panes; none, the calling pane's window.
	r, err := listScope(a, usage, "window")
	if err != nil {
		return err
	}
	panes, err := describe(socket, r)
	if err != nil {
		return err
	}
	if a.flags["--json"] {
		if panes == nil {
			panes = []protocol.PaneInfo{}
		}
		return printJSON(panes)
	}
	if f, ok := a.values["-F"]; ok {
		for _, p := range panes {
			fmt.Println(format.Expand(f, format.Vars(p)))
		}
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tWINDOW\tSIZE\tCOMMAND\tDIRECTORY\t")
	for _, p := range panes {
		note := ""
		if p.Active && p.WindowActive {
			note = "(active)"
		}
		fmt.Fprintf(w, "%s:%d\t%d.%d\t%dx%d\t%s\t%s\t%s\n", p.Session, p.Pane, p.Window, p.Index, p.Width, p.Height, p.Command, projectDir(p.Path), note)
	}
	return w.Flush()
}

func runListWindows(socket string, args []string) error {
	const usage = "usage: bmux list-windows [TARGET] [-a] [-F FORMAT | --json]"
	a, err := parseLayoutArgs(args, usage, []string{"-F"}, []string{"-a", "--json"})
	if err != nil {
		return err
	}
	r, err := listScope(a, usage, "session")
	if err != nil {
		return err
	}
	panes, err := describe(socket, r)
	if err != nil {
		return err
	}
	// A window is described by its focused pane, as in tmux, where window
	// formats can use the active pane's variables.
	var windows []protocol.PaneInfo
	for _, p := range panes {
		if p.Active {
			windows = append(windows, p)
		}
	}
	if a.flags["--json"] {
		if windows == nil {
			windows = []protocol.PaneInfo{}
		}
		return printJSON(windows)
	}
	if f, ok := a.values["-F"]; ok {
		for _, p := range windows {
			fmt.Println(format.Expand(f, format.Vars(p)))
		}
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "WINDOW\tNAME\tPANES\t")
	for _, p := range windows {
		var notes []string
		if p.WindowActive {
			notes = append(notes, "(active)")
		}
		if p.WindowZoomed {
			notes = append(notes, "(zoomed)")
		}
		fmt.Fprintf(w, "%s:%d.\t%s\t%d\t%s\n", p.Session, p.Window, p.WindowName, p.WindowPanes, strings.Join(notes, " "))
	}
	return w.Flush()
}

func runDisplayMessage(socket string, args []string) error {
	const usage = "usage: bmux display-message [TARGET] [-p] FORMAT"
	a, err := parseLayoutArgs(args, usage, nil, []string{"-p"})
	if err != nil {
		return err
	}
	var target string
	switch len(a.words) {
	case 1:
	case 2:
		target = a.words[0]
	default:
		return fmt.Errorf("%s (quote FORMAT if it has spaces)", usage)
	}
	r, err := describeRequest("pane", target)
	if err != nil {
		return err
	}
	panes, err := describe(socket, r)
	if err != nil {
		return err
	}
	if len(panes) != 1 {
		return fmt.Errorf("pane not found")
	}
	text := format.Expand(a.words[len(a.words)-1], format.Vars(panes[0]))
	if a.flags["-p"] {
		fmt.Println(text)
		return nil
	}
	// The status line holds one line.
	text = strings.Join(strings.Fields(text), " ")
	_, err = client.Request(socket, protocol.Request{Op: "display-message", Name: panes[0].Session, SessionID: panes[0].SessionID, Text: text})
	return err
}

// projectDir writes dir with ~ for the home directory.
func projectDir(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(dir, home); ok && (rest == "" || rest[0] == '/') {
			return "~" + rest
		}
	}
	return dir
}
