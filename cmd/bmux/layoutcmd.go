package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Layout commands change a session's panes and windows from a script, the
// way their keys do. The target defaults to the pane the command runs in.

// layoutArgs are a layout command's arguments: positional words, -x VALUE
// and -x flags in any order, and a command after --.
type layoutArgs struct {
	words   []string
	values  map[string]string
	flags   map[string]bool
	command []string
}

// parseLayoutArgs reads args; valued lists the flags that take a value,
// switches the ones that don't.
func parseLayoutArgs(args []string, usage string, valued, switches []string) (layoutArgs, error) {
	a := layoutArgs{values: map[string]string{}, flags: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			a.command = args[i+1:]
			if len(a.command) == 0 {
				return a, fmt.Errorf("-- needs a command; %s", usage)
			}
			return a, nil
		case slices.Contains(valued, arg):
			if i+1 == len(args) {
				return a, fmt.Errorf("%s needs a value; %s", arg, usage)
			}
			i++
			a.values[arg] = args[i]
		case slices.Contains(switches, arg):
			a.flags[arg] = true
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			return a, fmt.Errorf("unknown flag %s; %s", arg, usage)
		default:
			a.words = append(a.words, arg)
		}
	}
	return a, nil
}

// layoutRequest is a layout-command request for kind on targetText, or on the
// calling pane when it's empty.
func layoutRequest(kind, targetText string) (protocol.Request, error) {
	r := protocol.Request{Op: "layout-command", Kind: kind}
	if targetText != "" {
		t, err := parseTarget(targetText)
		if err != nil {
			return r, err
		}
		t.request(&r)
		return r, nil
	}
	session := os.Getenv("BMUX_SESSION")
	if session == "" {
		return r, fmt.Errorf("not inside a bmux pane; name a TARGET (%s)", targetForms)
	}
	// The ID survives renames, like bmux focus and bmux agent.
	r.Name = session
	r.SessionID, _ = strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
	r.PaneID, _ = strconv.Atoi(os.Getenv("BMUX_PANE"))
	return r, nil
}

// absDir makes -c DIR absolute against this command's directory.
func absDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return abs, nil
}

// oneTarget returns the single optional target among words.
func oneTarget(words []string, usage string) (string, error) {
	switch len(words) {
	case 0:
		return "", nil
	case 1:
		return words[0], nil
	}
	return "", fmt.Errorf("one target only; %s", usage)
}

func splitRequest(args []string) (protocol.Request, error) {
	const usage = "usage: bmux split [TARGET] [-h | -v] [-c DIR] [-d] [-- COMMAND...]"
	a, err := parseLayoutArgs(args, usage, []string{"-c"}, []string{"-h", "-v", "-d"})
	if err != nil {
		return protocol.Request{}, err
	}
	if a.flags["-h"] && a.flags["-v"] {
		return protocol.Request{}, fmt.Errorf("-h splits side by side and -v stacked; pick one")
	}
	target, err := oneTarget(a.words, usage)
	if err != nil {
		return protocol.Request{}, err
	}
	r, err := layoutRequest("split", target)
	if err != nil {
		return r, err
	}
	r.Direction = "vertical" // stacked, like tmux's default
	if a.flags["-h"] {
		r.Direction = "horizontal"
	}
	r.Dir, err = absDir(a.values["-c"])
	r.Detach, r.Args = a.flags["-d"], a.command
	return r, err
}

func newWindowRequest(args []string) (protocol.Request, error) {
	const usage = "usage: bmux new-window [TARGET] [-n NAME] [-c DIR] [-d] [-- COMMAND...]"
	a, err := parseLayoutArgs(args, usage, []string{"-n", "-c"}, []string{"-d"})
	if err != nil {
		return protocol.Request{}, err
	}
	target, err := oneTarget(a.words, usage)
	if err != nil {
		return protocol.Request{}, err
	}
	r, err := layoutRequest("new-window", target)
	if err != nil {
		return r, err
	}
	r.NewName = a.values["-n"]
	r.Dir, err = absDir(a.values["-c"])
	r.Detach, r.Args = a.flags["-d"], a.command
	return r, err
}

// targetOnlyRequest is select, kill-pane, kill-window and zoom: a target
// and nothing else. required refuses the calling pane as a default.
func targetOnlyRequest(kind string, args []string, required bool) (protocol.Request, error) {
	usage := "usage: bmux " + kind + " TARGET"
	if !required {
		usage = "usage: bmux " + kind + " [TARGET]"
	}
	a, err := parseLayoutArgs(args, usage, nil, nil)
	if err != nil {
		return protocol.Request{}, err
	}
	target, err := oneTarget(a.words, usage)
	if err != nil {
		return protocol.Request{}, err
	}
	if required && target == "" {
		return protocol.Request{}, fmt.Errorf("%s needs a TARGET (%s), so nothing is closed by accident", kind, targetForms)
	}
	return layoutRequest(kind, target)
}

func resizeRequest(args []string) (protocol.Request, error) {
	const usage = "usage: bmux resize [TARGET] left|right|up|down [CELLS]"
	a, err := parseLayoutArgs(args, usage, nil, nil)
	if err != nil {
		return protocol.Request{}, err
	}
	directions := []string{"left", "right", "up", "down"}
	at := slices.IndexFunc(a.words, func(w string) bool { return slices.Contains(directions, w) })
	if at < 0 || at > 1 || len(a.words) > at+2 {
		return protocol.Request{}, fmt.Errorf("%s", usage)
	}
	target := ""
	if at == 1 {
		target = a.words[0]
	}
	r, err := layoutRequest("resize", target)
	if err != nil {
		return r, err
	}
	r.Direction = a.words[at]
	if at+1 < len(a.words) {
		cells, err := strconv.Atoi(a.words[at+1])
		if err != nil || cells < 1 || cells > protocol.MaxTerminalWidth {
			return r, fmt.Errorf("CELLS is a number of columns or rows, 1 to %d", protocol.MaxTerminalWidth)
		}
		r.Delta = cells
	}
	return r, nil
}

func renameWindowRequest(args []string) (protocol.Request, error) {
	const usage = "usage: bmux rename-window [TARGET] NAME"
	a, err := parseLayoutArgs(args, usage, nil, nil)
	if err != nil {
		return protocol.Request{}, err
	}
	var target, name string
	switch len(a.words) {
	case 1:
		name = a.words[0]
	case 2:
		target, name = a.words[0], a.words[1]
	default:
		return protocol.Request{}, fmt.Errorf("%s", usage)
	}
	r, err := layoutRequest("rename-window", target)
	r.NewName = name
	return r, err
}

// runLayoutCommand sends a layout command built by build; split and
// new-window print the new pane's target for the next command to use.
func runLayoutCommand(build func([]string) (protocol.Request, error), printPane bool) func(string, []string) error {
	return func(socket string, args []string) error {
		r, err := build(args)
		if err != nil {
			return err
		}
		response, err := client.Request(socket, r)
		if err != nil {
			return err
		}
		if printPane && response.Pane != nil {
			fmt.Println(paneTarget(r.Name, response.Pane.Pane))
		}
		return nil
	}
}

func targetOnly(kind string, required bool) func([]string) (protocol.Request, error) {
	return func(args []string) (protocol.Request, error) { return targetOnlyRequest(kind, args, required) }
}
