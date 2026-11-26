package integration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testEnv(t *testing.T, onPath ...string) Env {
	t.Helper()
	home := t.TempDir()
	return Env{Home: home, Getenv: func(string) string { return "" }, LookPath: func(name string) (string, error) {
		for _, p := range onPath {
			if p == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}}
}

const userSettings = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "say done"
          }
        ]
      }
    ]
  },
  "permissions": {
    "allow": [
      "Bash(go test:*)"
    ]
  }
}
`

func TestClaudeIntegrationLifecycleKeepsUserSettings(t *testing.T) {
	env := testEnv(t)
	a, _ := Find("claude")
	if status, err := a.Status(env); status != NotFound || err != nil {
		t.Fatalf("without claude on PATH: %v %v", status, err)
	}
	env = testEnv(t, "claude")
	dir := filepath.Join(env.Home, ".claude")
	settings := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(userSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.Status(env); status != Available {
		t.Fatalf("claude on PATH: %v", status)
	}
	for range 2 { // reinstalling replaces, never duplicates
		if err := a.Install(env); err != nil {
			t.Fatal(err)
		}
	}
	if status, err := a.Status(env); status != Installed || err != nil {
		t.Fatalf("after install: %v %v", status, err)
	}
	data, _ := os.ReadFile(settings)
	text := string(data)
	if strings.Count(text, hookScriptName) != len(claude.events) || !strings.Contains(text, "say done") {
		t.Fatalf("hooks not added exactly once beside the user's:\n%s", text)
	}
	if strings.Index(text, `"model"`) > strings.Index(text, `"hooks"`) || strings.Index(text, `"hooks"`) > strings.Index(text, `"permissions"`) {
		t.Fatalf("settings were reordered:\n%s", text)
	}
	if info, _ := os.Stat(settings); info.Mode().Perm() != 0o600 {
		t.Fatalf("settings mode changed to %v", info.Mode().Perm())
	}
	script, _ := os.ReadFile(filepath.Join(dir, "hooks", hookScriptName))
	if !strings.HasPrefix(string(script), fmt.Sprintf("#!/bin/sh\n# bmux-integration: claude v%d\n", a.Version)) || !strings.Contains(string(script), "bmux integration hook claude") {
		t.Fatalf("script:\n%s", script)
	}
	// The skill is a loadable SKILL.md: frontmatter first, then bmux's header.
	skill := filepath.Join(dir, "skills", "bmux", "SKILL.md")
	if data, _ := os.ReadFile(skill); !strings.HasPrefix(string(data), "---\nname: bmux\n") || !strings.Contains(string(data), fmt.Sprintf("bmux-integration: claude v%d", a.Version)) {
		t.Fatalf("skill:\n%s", data)
	}
	// A missing skill is an update, not a working install.
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.Status(env); status != Outdated {
		t.Fatalf("without the skill: %v", status)
	}
	if err := a.Install(env); err != nil {
		t.Fatal(err)
	}

	// A newer bmux sees the installed version as an update.
	newer := a
	newer.Version++
	if status, _ := newer.Status(env); status != Outdated {
		t.Fatalf("older version installed: %v", status)
	}
	if err := a.Uninstall(env); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(settings); string(data) != userSettings {
		t.Fatalf("uninstall didn't restore the user's settings:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks", hookScriptName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall left the script")
	}
	if _, err := os.Stat(filepath.Dir(skill)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall left the skill")
	}
	if status, _ := a.Status(env); status != Available {
		t.Fatalf("after uninstall: %v", status)
	}
}

func TestEditHooksRemovesOnlyBmuxHandlers(t *testing.T) {
	mixed := `{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"say done"},{"type":"command","command":"/h/hooks/bmux-agent-state.sh"}]}],"SessionEnd":[{"hooks":[{"type":"command","command":"/h/hooks/bmux-agent-state.sh"}]}]}}`
	out, err := editHooks([]byte(mixed), hookScriptName, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"hooks\": {\n    \"Stop\": [\n      {\n        \"matcher\": \"\",\n        \"hooks\": [\n          {\n            \"type\": \"command\",\n            \"command\": \"say done\"\n          }\n        ]\n      }\n    ]\n  }\n}\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if out, _ := editHooks([]byte(`{"theme":"dark","hooks":{"Stop":[{"hooks":[{"command":"bmux-agent-state.sh"}]}]}}`), hookScriptName, "", nil); string(out) != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Fatalf("emptied hooks table kept: %s", out)
	}
	if _, err := editHooks([]byte(`{"hooks": [}`), hookScriptName, "x", []string{"Stop"}); err == nil {
		t.Fatal("invalid JSON was rewritten")
	}
}

func TestHookEventsBecomeReports(t *testing.T) {
	for _, tc := range []struct {
		agent, input string
		want         Report
		ok           bool
	}{
		{"claude", `{"hook_event_name":"SessionStart","session_id":"3f2a-b9"}`, Report{"idle", "claude", []string{"claude", "--resume", "3f2a-b9"}}, true},
		{"claude", `{"hook_event_name":"UserPromptSubmit","session_id":"s1"}`, Report{"working", "claude", []string{"claude", "--resume", "s1"}}, true},
		{"claude", `{"hook_event_name":"PermissionRequest","session_id":"s1"}`, Report{"blocked", "claude", []string{"claude", "--resume", "s1"}}, true},
		{"claude", `{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, Report{State: "blocked", Label: "claude"}, true},
		{"claude", `{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, Report{}, false},
		{"claude", `{"hook_event_name":"PostToolUse","session_id":"s1"}`, Report{"working", "claude", []string{"claude", "--resume", "s1"}}, true},
		{"claude", `{"hook_event_name":"Stop","session_id":"$(rm -rf ~)"}`, Report{State: "idle", Label: "claude"}, true},
		{"claude", `{"hook_event_name":"SessionEnd","session_id":"s1"}`, Report{State: "clear", Label: "claude"}, true},
		{"claude", `{"hook_event_name":"SubagentStop"}`, Report{}, false},
		{"codex", `{"hook_event_name":"Interrupt","session_id":"thr_1"}`, Report{"idle", "codex", []string{"codex", "resume", "thr_1"}}, true},
		{"codex", `{"hook_event_name":"PermissionRequest","session_id":"thr_1"}`, Report{"blocked", "codex", []string{"codex", "resume", "thr_1"}}, true},
		{"claude", `not json`, Report{}, false},
		{"gemini", `{"hook_event_name":"Stop"}`, Report{}, false},
	} {
		got, ok := HookReport(tc.agent, strings.NewReader(tc.input))
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %s: %+v %v, want %+v %v", tc.agent, tc.input, got, ok, tc.want, tc.ok)
		}
	}
}

func TestOpencodeIntegrationLifecycle(t *testing.T) {
	for _, xdg := range []bool{false, true} {
		t.Run(fmt.Sprintf("xdg=%v", xdg), func(t *testing.T) {
			e := testEnv(t, "opencode")
			if xdg {
				e.Getenv = func(name string) string {
					if name == "XDG_CONFIG_HOME" {
						return filepath.Join(e.Home, "config")
					}
					return ""
				}
			}
			a, _ := Find("opencode")
			assertStatus := func(want Status) {
				t.Helper()
				if got, err := a.Status(e); got != want || err != nil {
					t.Fatalf("status: %v %v; want %v", got, err, want)
				}
			}
			assertStatus(Available)
			dir := a.ConfigDir(e)
			file := filepath.Join(dir, "plugins", "bmux-agent-state.js")
			if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(dir, "opencode.jsonc")
			other := filepath.Join(dir, "plugins", "user.js")
			for _, p := range []string{config, other} {
				if err := os.WriteFile(p, []byte("// untouched\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// An installed V1-only generated plugin is an update, not current.
			if err := os.WriteFile(file, []byte(headerLines("//", "opencode", 1)+"export const BmuxAgentState = async () => ({});\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			assertStatus(Outdated)
			for range 2 {
				if err := a.Install(e); err != nil {
					t.Fatal(err)
				}
				assertStatus(Installed)
			}
			data, err := os.ReadFile(file)
			if err != nil || string(data) != opencodePlugin(headerLines("//", "opencode", 2)) {
				t.Fatalf("generated plugin: %v\n%s", err, data)
			}
			if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("mode changed: %v %v", info, err)
			}
			for range 2 {
				if err := a.Uninstall(e); err != nil {
					t.Fatal(err)
				}
				assertStatus(Available)
			}
			if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("plugin left after uninstall: %v", err)
			}
			// Never overwrite or remove a user's file with the same name.
			unmanaged := "export default { id: 'user', setup() {} };\n"
			if err := os.WriteFile(file, []byte(unmanaged), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := a.Install(e); err == nil || !strings.Contains(err.Error(), "unmanaged") {
				t.Fatalf("unmanaged install accepted: %v", err)
			}
			if err := a.Uninstall(e); err != nil {
				t.Fatal(err)
			}
			if data, _ := os.ReadFile(file); string(data) != unmanaged {
				t.Fatal("unmanaged plugin changed")
			}
			for _, p := range []string{config, other} {
				if data, _ := os.ReadFile(p); string(data) != "// untouched\n" {
					t.Fatalf("unrelated file changed: %s", p)
				}
			}
		})
	}
}
