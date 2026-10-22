package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/josephferrero/bmux/docs"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/version"
)

// command is one `bmux NAME` subcommand. This table drives dispatch, --help,
// bmux guide's command reference and bmux capabilities, so a command can't
// exist without being documented to people and agents alike.
type command struct {
	name, usage, summary string
	server               serverUse
	run                  func(socket string, args []string) error
}

type serverUse int

const (
	// noSocket commands need no runtime directory or server.
	noSocket serverUse = iota
	// noServer commands talk to a running server and never start one, such
	// as reports from agent hooks.
	noServer
	// startServer commands start the server when it isn't running.
	startServer
)

// commands is filled in init: help and guide read the table they're in.
var commands []command

func init() {
	commands = []command{
		{"", "", "Create and attach a fresh session in this directory (bmux attach returns to one)", startServer, runDefault},
		{"new", "[--dir PATH] [--detach] NAME", "Create a shell session", startServer, runNew},
		{"attach", "[NAME]", "Attach by name, or to the most recently used session", startServer, runAttach},
		{"project", "[DIR]", "Open DIR's session, creating it if needed; without DIR, pick a project", startServer, runProject},
		{"list", "", "List sessions", startServer, runList},
		{"rename", "OLD NEW", "Rename a session", startServer, runRename},
		{"kill", "NAME", "Terminate a session and all its panes", startServer, runKill},
		{"focus", "left|right|up|down", "Move pane focus (for editors at an edge)", startServer, runFocus},
		{"zoomed", "", "Print true if the calling pane's window is zoomed, else false (for editors)", startServer, runZoomed},
		{"agents", "[--json]", "List agent panes as SESSION:PANE targets, with state and seq", startServer, runAgents},
		{"capture", "[--json] TARGET", "Print a pane's visible text; TARGET is SESSION, SESSION:PANE, SESSION:WINDOW. or SESSION:WINDOW.N", startServer, runCapture},
		{"send", "TARGET [--if-seq N] (--key KEY | --text TEXT)...", "Type into a pane: keys like enter, down or C-c, and text", startServer, runSend},
		{"wait-agent", "[--state S,...] [--since SEQ] [--timeout D | --notify SESSION:PANE] [TARGET...]", "Wait until an agent pane is blocked or idle; print it, or with --notify type it into a supervising agent", startServer, runWaitAgent},
		{"split", "[TARGET] [-h | -v] [-c DIR] [-d] [-- COMMAND...]", "Split a pane (-h side by side, -v stacked) in its directory or DIR, type COMMAND into the new shell, print the new pane", noServer, runLayoutCommand(splitRequest, true)},
		{"new-window", "[TARGET] [-n NAME] [-c DIR] [-d] [-- COMMAND...]", "Open a window in the target's session; prints the new pane", noServer, runLayoutCommand(newWindowRequest, true)},
		{"select", "TARGET", "Focus a pane, bringing its window forward, or a window (SESSION:WINDOW.)", noServer, runLayoutCommand(targetOnly("select", true), false)},
		{"kill-pane", "TARGET", "Close a pane and its programs without asking", noServer, runLayoutCommand(targetOnly("kill-pane", true), false)},
		{"kill-window", "TARGET", "Close the target's window and its programs without asking", noServer, runLayoutCommand(targetOnly("kill-window", true), false)},
		{"resize", "[TARGET] left|right|up|down [CELLS]", "Move a pane's divider, 5 cells unless CELLS says", noServer, runLayoutCommand(resizeRequest, false)},
		{"zoom", "[TARGET]", "Zoom or unzoom a pane", noServer, runLayoutCommand(targetOnly("zoom", false), false)},
		{"rename-window", "[TARGET] NAME", "Rename the target's window; an empty NAME shows its pane's title", noServer, runLayoutCommand(renameWindowRequest, false)},
		{"list-panes", "[TARGET] [-a] [-F FORMAT | --json]", "List the panes of a window (SESSION:WINDOW. or a pane), a session, or every session (-a); -F formats each line with #{...} variables", noServer, runListPanes},
		{"list-windows", "[TARGET] [-a] [-F FORMAT | --json]", "List a session's windows, or every session's (-a)", noServer, runListWindows},
		{"display-message", "[TARGET] [-p] FORMAT", "Expand FORMAT's #{...} variables for a pane; -p prints it, otherwise it shows in the status line of the session's clients", noServer, runDisplayMessage},
		{"run-shell", "[-t TARGET] [-b] COMMAND", "Run a shell command for a pane: its #{...} variables, directory and BMUX_* environment; -b doesn't wait", noServer, runRunShell},
		{"if-shell", "[-t TARGET] CONDITION COMMAND [ELSE]", "Run CONDITION for a pane, then COMMAND if it succeeded or ELSE if not", noServer, runIfShell},
		{"agent", "STATE [NAME] [-- CMD...]", "Report this pane's agent: working, blocked, idle, clear", noServer, reportAgent},
		{"integration", "status|install|uninstall [AGENT]", "Make claude, codex, omp, opencode and pi report exact state", noServer, func(socket string, args []string) error {
			// Writes agents' config files and reports from their hooks.
			return runIntegration(socket, args, os.Stdin, os.Stdout)
		}},
		{"layout", "save [SESSION] | trust [DIR]", "Save a session's windows as its folder's .bmux.toml, or trust a folder's file so its commands run", noServer, runLayout},
		{"restart", "", "Save sessions, restart the server, restore them", noServer, runRestart},
		{"update", "[--check]", "Install the newest release, checked against its checksums; --check only reports", noServer, runUpdate},
		{"reload", "", "Reread the config file (also Ctrl-a r)", noServer, runReload},
		{"guide", "", "How agents read, drive and watch panes (Markdown)", noSocket, runGuide},
		{"capabilities", "", "Commands, protocol version and the calling pane, as JSON", noSocket, runCapabilities},
		{"default-config", "", "Print every setting with its default", noSocket, func(string, []string) error {
			fmt.Print(config.DefaultFile)
			return nil
		}},
		{"version", "", "Print the version", noSocket, func(string, []string) error {
			fmt.Println("bmux", version.String())
			return nil
		}},
		{"help", "", "Print this help", noSocket, func(string, []string) error {
			fmt.Println(helpText())
			return nil
		}},
	}
}

func findCommand(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

// commandReference renders the command table as Markdown for bmux guide.
func commandReference() string {
	var b strings.Builder
	b.WriteString("## Command reference\n\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "- `%s` — %s\n", strings.TrimSpace("bmux "+c.name+" "+c.usage), c.summary)
	}
	return b.String()
}

func runGuide(_ string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bmux guide")
	}
	fmt.Print(docs.AgentGuide)
	fmt.Print("\n" + commandReference())
	return nil
}

type capabilityCommand struct {
	Name    string `json:"name"`
	Usage   string `json:"usage"`
	Summary string `json:"summary"`
}

type capabilityPane struct {
	Session   string `json:"session"`
	SessionID uint64 `json:"session_id,omitempty"`
	Pane      int    `json:"pane"`
	Target    string `json:"target"`
}

type capabilities struct {
	Version     string              `json:"version"`
	Protocol    int                 `json:"protocol"`
	Guide       string              `json:"guide"`
	Commands    []capabilityCommand `json:"commands"`
	AgentStates []string            `json:"agent_states"`
	// Pane is where this command runs; null outside bmux.
	Pane *capabilityPane `json:"pane"`
}

func runCapabilities(_ string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: bmux capabilities")
	}
	result := capabilities{
		Version: version.String(), Protocol: protocol.Version, Guide: "bmux guide",
		AgentStates: []string{protocol.AgentWorking, protocol.AgentBlocked, protocol.AgentIdle},
	}
	for _, c := range commands {
		if c.name != "" {
			result.Commands = append(result.Commands, capabilityCommand{c.name, strings.TrimSpace("bmux " + c.name + " " + c.usage), c.summary})
		}
	}
	if session := os.Getenv("BMUX_SESSION"); session != "" {
		id, _ := strconv.ParseUint(os.Getenv("BMUX_SESSION_ID"), 10, 64)
		pane, _ := strconv.Atoi(os.Getenv("BMUX_PANE"))
		result.Pane = &capabilityPane{session, id, pane, paneTarget(session, pane)}
	}
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	return out.Encode(result)
}
