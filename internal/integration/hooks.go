package integration

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Claude Code and Codex run commands from JSON hook tables. Their entries all
// run one managed script, which hands the event on stdin to
// `bmux integration hook AGENT`; the mapping from events to states lives here
// in Go, so the script stays trivial and nothing needs jq.

const hookScriptName = "bmux-agent-state.sh"

// hookAgent is an agent whose integration is hook table entries plus a script.
type hookAgent struct {
	name     string
	version  int
	settings string   // the JSON file holding the hooks table, in the config dir
	events   []string // events whose hooks run the script
	states   func(event hookEvent) string
	resume   func(session string) []string
}

func (h hookAgent) script(dir string) string { return filepath.Join(dir, "hooks", hookScriptName) }

func (h hookAgent) scriptText() []byte {
	return []byte("#!/bin/sh\n" + headerLines("#", h.name, h.version) +
		"# Reports this agent's state to the bmux pane it runs in; elsewhere it does nothing.\n" +
		"[ -n \"$BMUX_SESSION\" ] || exit 0\n" +
		"command -v bmux >/dev/null 2>&1 || exit 0\n" +
		"exec bmux integration hook " + h.name + " >/dev/null 2>&1\n")
}

func (h hookAgent) install(dir string) error {
	if err := writeFile(h.script(dir), h.scriptText(), 0o755); err != nil {
		return err
	}
	return h.editSettings(dir, h.events)
}

func (h hookAgent) uninstall(dir string) error {
	if err := h.editSettings(dir, nil); err != nil {
		return err
	}
	return removeFile(h.script(dir), h.name)
}

func (h hookAgent) installed(dir string) (int, error) {
	version, err := fileVersion(h.script(dir), h.name)
	if err != nil || version == 0 {
		return 0, err
	}
	data, err := os.ReadFile(filepath.Join(dir, h.settings))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if !hasHooks(data, hookScriptName, h.events) {
		return 0, nil // the script without its entries does nothing
	}
	return version, nil
}

// editSettings removes bmux's entries from the hooks file, then adds one for
// each event. An absent file only gets created when there is something to add.
func (h hookAgent) editSettings(dir string, add []string) error {
	path := filepath.Join(dir, h.settings)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if len(add) == 0 {
			return nil
		}
		data, err = nil, nil
	}
	if err != nil {
		return err
	}
	edited, err := editHooks(data, hookScriptName, shellQuote(h.script(dir)), add)
	if err != nil {
		return &os.PathError{Op: "edit", Path: path, Err: err}
	}
	if string(edited) == string(data) {
		return nil
	}
	return writeFile(path, edited, 0o600)
}

func (h hookAgent) agent(label, command, configVar, configDir string, notes string) Agent {
	return Agent{
		Name: h.name, Label: label, Command: command, Version: h.version, Notes: notes,
		configDir: dirFromEnv(configVar, configDir),
		install:   h.install, uninstall: h.uninstall, installed: h.installed,
	}
}

// hookEvent is the part of a hook's stdin JSON bmux reads.
type hookEvent struct {
	Event            string `json:"hook_event_name"`
	Session          string `json:"session_id"`
	NotificationType string `json:"notification_type"`
}

// sessionID is what agents use for session ids: letters, digits, - and _.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Report is what a hook tells bmux: a `bmux agent` state, the sidebar label,
// and the command that resumes this exact session (nil when unknown).
type Report struct {
	State, Label string
	Resume       []string
}

// HookReport turns a hook event read from input into a report for agent.
// ok is false for events that don't change anything, and for unknown agents.
func HookReport(agent string, input io.Reader) (Report, bool) {
	var h hookAgent
	switch agent {
	case claude.name:
		h = claude
	case codex.name:
		h = codex
	default:
		return Report{}, false
	}
	var event hookEvent
	if json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&event) != nil {
		return Report{}, false
	}
	state := h.states(event)
	if state == "" {
		return Report{}, false
	}
	r := Report{State: state, Label: agent}
	if state != "clear" && sessionID.MatchString(event.Session) {
		r.Resume = h.resume(event.Session)
	}
	return r, true
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var claude = hookAgent{
	name: "claude", version: 1, settings: "settings.json",
	events: []string{"SessionStart", "UserPromptSubmit", "PermissionRequest", "PostToolUse", "PostToolUseFailure",
		"Notification", "Elicitation", "ElicitationResult", "Stop", "StopFailure", "SessionEnd"},
	states: func(e hookEvent) string {
		switch e.Event {
		case "UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "ElicitationResult":
			return "working"
		case "PermissionRequest", "Elicitation":
			return "blocked"
		case "Notification":
			switch e.NotificationType {
			case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog", "agent_needs_input":
				return "blocked"
			}
		case "SessionStart", "Stop", "StopFailure":
			return "idle"
		case "SessionEnd":
			return "clear"
		}
		return ""
	},
	resume: func(session string) []string { return []string{"claude", "--resume", session} },
}

var codex = hookAgent{
	name: "codex", version: 1, settings: "hooks.json",
	events: []string{"SessionStart", "UserPromptSubmit", "PermissionRequest", "PostToolUse", "Stop", "Interrupt", "SessionEnd"},
	states: func(e hookEvent) string {
		switch e.Event {
		case "UserPromptSubmit", "PostToolUse":
			return "working"
		case "PermissionRequest":
			return "blocked"
		case "SessionStart", "Stop", "Interrupt":
			return "idle"
		case "SessionEnd":
			return "clear"
		}
		return ""
	},
	resume: func(session string) []string { return []string{"codex", "resume", session} },
}
