package integration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const userAgyHooks = `{
  "lint-checker": {
    "PostToolUse": [
      {
        "matcher": "run_command",
        "hooks": [
          {
            "type": "command",
            "command": "./scripts/lint.sh"
          }
        ]
      }
    ]
  },
  "reminder": {
    "PreInvocation": [
      {
        "type": "command",
        "command": "./scripts/reminder.sh"
      }
    ]
  }
}
`

func TestAgyIntegrationLifecycleKeepsUserHooks(t *testing.T) {
	env := testEnv(t)
	a, ok := Find("agy")
	if !ok {
		t.Fatal("agy integration not registered")
	}
	if status, err := a.Status(env); status != NotFound || err != nil {
		t.Fatalf("without agy on PATH: %v %v", status, err)
	}

	env = testEnv(t, "agy")
	dir := a.ConfigDir(env)
	wantDir := filepath.Join(env.Home, ".gemini", "config")
	if dir != wantDir {
		t.Fatalf("ConfigDir = %q, want %q", dir, wantDir)
	}
	hooksPath := filepath.Join(dir, agyHooksFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(userAgyHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.Status(env); status != Available {
		t.Fatalf("agy on PATH: %v", status)
	}

	for range 2 { // reinstalling replaces cleanly without duplicates
		if err := a.Install(env); err != nil {
			t.Fatal(err)
		}
	}
	if status, err := a.Status(env); status != Installed || err != nil {
		t.Fatalf("after install: %v %v", status, err)
	}

	data, _ := os.ReadFile(hooksPath)
	text := string(data)
	if !strings.Contains(text, `"bmux-agent-state"`) || !strings.Contains(text, `"lint-checker"`) || !strings.Contains(text, `"reminder"`) {
		t.Fatalf("hooks.json missing expected entries:\n%s", text)
	}
	if strings.Index(text, `"lint-checker"`) > strings.Index(text, `"reminder"`) || strings.Index(text, `"reminder"`) > strings.Index(text, `"bmux-agent-state"`) {
		t.Fatalf("hooks.json reordered user hooks:\n%s", text)
	}
	if !strings.Contains(text, `"SessionStart"`) || !strings.Contains(text, `"PreInvocation"`) || !strings.Contains(text, `"PostToolUse"`) || !strings.Contains(text, `"Stop"`) {
		t.Fatalf("hooks.json missing expected events:\n%s", text)
	}
	if strings.Contains(text, `"PreToolUse"`) {
		t.Fatalf("hooks.json should not register PreToolUse:\n%s", text)
	}
	if info, _ := os.Stat(hooksPath); info.Mode().Perm() != 0o600 {
		t.Fatalf("hooks.json mode changed to %v", info.Mode().Perm())
	}

	scriptPath := filepath.Join(dir, "hooks", hookScriptName)
	script, _ := os.ReadFile(scriptPath)
	if !strings.HasPrefix(string(script), fmt.Sprintf("#!/bin/sh\n# bmux-integration: agy v%d\n", a.Version)) || !strings.Contains(string(script), "bmux integration hook agy") {
		t.Fatalf("script:\n%s", script)
	}

	// Verify the script emits valid JSON "{}" even outside a bmux session.
	cmd := exec.Command("/bin/sh", scriptPath)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + env.Home}
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "{}\n" {
		t.Fatalf("script execution: out=%q err=%v", out, err)
	}

	skill := skillPath(dir)
	if skillData, _ := os.ReadFile(skill); !strings.HasPrefix(string(skillData), "---\nname: bmux\n") || !strings.Contains(string(skillData), fmt.Sprintf("bmux-integration: agy v%d", a.Version)) {
		t.Fatalf("skill:\n%s", skillData)
	}
	if err := os.Remove(skill); err != nil {
		t.Fatal(err)
	}
	if err := a.Install(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("reinstall did not restore skill: %v", err)
	}

	newer := a
	newer.Version++
	if status, _ := newer.Status(env); status != Outdated {
		t.Fatalf("older version installed: %v", status)
	}

	if err := a.Uninstall(env); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(hooksPath); string(data) != userAgyHooks {
		t.Fatalf("uninstall did not restore user hooks:\n%s", data)
	}
	if _, err := os.Stat(scriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall left hook script")
	}
	if _, err := os.Stat(filepath.Dir(skill)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("uninstall left skill directory")
	}
	if status, _ := a.Status(env); status != Available {
		t.Fatalf("after uninstall: %v", status)
	}
}

func TestAgyHookEventsBecomeReports(t *testing.T) {
	const cid = "ec33ebf9-0cba-4100-8142-c61503f6c587"
	for _, tc := range []struct {
		input string
		want  Report
		ok    bool
	}{
		{
			fmt.Sprintf(`{"hookEventName":"SessionStart","conversationId":%q}`, cid),
			Report{"idle", "agy", []string{"agy", "--resume", cid}},
			true,
		},
		{
			fmt.Sprintf(`{"hookEventName":"PreInvocation","conversationId":%q}`, cid),
			Report{"working", "agy", []string{"agy", "--resume", cid}},
			true,
		},
		{
			fmt.Sprintf(`{"hookEventName":"PostToolUse","conversationId":%q}`, cid),
			Report{"working", "agy", []string{"agy", "--resume", cid}},
			true,
		},
		{
			fmt.Sprintf(`{"hookEventName":"PermissionRequest","conversationId":%q}`, cid),
			Report{"blocked", "agy", []string{"agy", "--resume", cid}},
			true,
		},
		{
			`{"hookEventName":"Stop","conversationId":"$(rm -rf ~)"}`,
			Report{State: "idle", Label: "agy"},
			true,
		},
		{
			`{"hookEventName":"PreToolUse","conversationId":"c1"}`,
			Report{},
			false,
		},
		{
			`not json`,
			Report{},
			false,
		},
	} {
		got, ok := HookReport("agy", strings.NewReader(tc.input))
		if ok != tc.ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v %v, want %+v %v", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}
