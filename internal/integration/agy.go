package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Antigravity hooks live in ~/.gemini/config/hooks.json under top-level named
// hook entries. SessionStart, PreInvocation, and Stop use flat handler arrays,
// while PostToolUse wraps handlers inside matcher groups. Every command hook
// must emit a valid JSON object on stdout.

const (
	agyHookName  = "bmux-agent-state"
	agyHooksFile = "hooks.json"
	agyVersion   = 1
)

var agyHook = hookAgent{
	name:     "agy",
	version:  agyVersion,
	settings: agyHooksFile,
	events:   []string{"SessionStart", "PreInvocation", "PostToolUse", "Stop"},
	states: func(e hookEvent) string {
		switch e.Event {
		case "PreInvocation", "PostToolUse":
			return "working"
		case "PermissionRequest":
			return "blocked"
		case "SessionStart", "Stop":
			return "idle"
		}
		return ""
	},
	resume: func(session string) []string { return []string{"agy", "--resume", session} },
}

func agyAgent() Agent {
	return Agent{
		Name:      "agy",
		Label:     "Antigravity",
		Command:   "agy",
		Version:   agyVersion,
		configDir: func(e Env) string { return filepath.Join(e.Home, ".gemini", "config") },
		install:   installAgy,
		uninstall: uninstallAgy,
		installed: installedAgy,
	}
}

func agyScriptPath(dir string) string {
	return filepath.Join(dir, "hooks", hookScriptName)
}

func agyScriptText() []byte {
	return []byte("#!/bin/sh\n" + headerLines("#", "agy", agyVersion) +
		"# Reports this agent's state to the bmux pane it runs in; elsewhere it does nothing.\n" +
		"if [ -n \"$BMUX_SESSION\" ] && command -v bmux >/dev/null 2>&1; then\n" +
		"  bmux integration hook agy >/dev/null 2>&1 || true\n" +
		"fi\n" +
		"printf '{}\\n'\n")
}

func installAgy(dir string) error {
	if err := writeFile(agyScriptPath(dir), agyScriptText(), 0o755); err != nil {
		return err
	}
	return editAgyHooksFile(dir, true)
}

func uninstallAgy(dir string) error {
	if err := editAgyHooksFile(dir, false); err != nil {
		return err
	}
	return removeFile(agyScriptPath(dir), "agy")
}

func installedAgy(dir string) (int, error) {
	version, err := fileVersion(agyScriptPath(dir), "agy")
	if err != nil || version == 0 {
		return 0, err
	}
	data, err := os.ReadFile(filepath.Join(dir, agyHooksFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if !hasAgyHooks(data, hookScriptName) {
		return 0, nil
	}
	return version, nil
}

func editAgyHooksFile(dir string, install bool) error {
	path := filepath.Join(dir, agyHooksFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if !install {
			return nil
		}
		data, err = nil, nil
	}
	if err != nil {
		return err
	}
	edited, err := editAgyHooks(data, shellQuote(agyScriptPath(dir)), install)
	if err != nil {
		return &os.PathError{Op: "edit", Path: path, Err: err}
	}
	if string(edited) == string(data) {
		return nil
	}
	return writeFile(path, edited, 0o600)
}

func editAgyHooks(data []byte, command string, install bool) ([]byte, error) {
	top, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	if !install {
		top = top.set(agyHookName, nil)
	} else {
		handler, _ := json.Marshal(struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}{"command", command})
		flatList := json.RawMessage("[" + string(handler) + "]")
		matcherGroup := object{
			{"matcher", json.RawMessage(`"*"`)},
			{"hooks", flatList},
		}.encode()
		groupedList := json.RawMessage("[" + string(matcherGroup) + "]")

		hookSpec := object{
			{"SessionStart", flatList},
			{"PreInvocation", flatList},
			{"PostToolUse", groupedList},
			{"Stop", flatList},
		}.encode()
		top = top.set(agyHookName, hookSpec)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, top.encode(), "", "  "); err != nil {
		return nil, fmt.Errorf("indent %s: %w", agyHooksFile, err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func hasAgyHooks(data []byte, marker string) bool {
	var all map[string]struct {
		SessionStart  []hookHandler `json:"SessionStart"`
		PreInvocation []hookHandler `json:"PreInvocation"`
		PostToolUse   []struct {
			Hooks []hookHandler `json:"hooks"`
		} `json:"PostToolUse"`
		Stop []hookHandler `json:"Stop"`
	}
	if json.Unmarshal(data, &all) != nil {
		return false
	}
	spec, ok := all[agyHookName]
	if !ok {
		return false
	}
	hasFlat := func(list []hookHandler) bool {
		for _, h := range list {
			if strings.Contains(h.Command, marker) {
				return true
			}
		}
		return false
	}
	hasPost := false
	for _, g := range spec.PostToolUse {
		if hasFlat(g.Hooks) {
			hasPost = true
			break
		}
	}
	return hasFlat(spec.SessionStart) && hasFlat(spec.PreInvocation) && hasPost && hasFlat(spec.Stop)
}
