package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// piDriver loads a pi/omp extension with a stand-in for the extension API
// and sends it [name, event] pairs with the given ctx.
const piDriver = `import extension from "./extension.mjs";
const handlers = {};
extension({ on: (name, fn) => { handlers[name] = fn; } });
const ctx = JSON.parse(process.argv[2]);
const sessionFile = ctx.sessionFile;
ctx.sessionManager = { getSessionFile: () => sessionFile };
ctx.isIdle = () => true;
for (const [name, event] of JSON.parse(process.argv[3])) {
  await handlers[name]?.(event, ctx);
  await new Promise((resolve) => setTimeout(resolve, 60));
}
`

// opencodeDriver loads an opencode plugin and sends it [type, properties] pairs.
const opencodeDriver = `import { BmuxAgentState } from "./extension.mjs";
const hooks = await BmuxAgentState({});
for (const [type, properties] of JSON.parse(process.argv[3])) {
  await hooks.event?.({ event: { type, properties } });
  await new Promise((resolve) => setTimeout(resolve, 60));
}
`

// runExtension loads an installed extension or plugin in node through driver,
// with a fake bmux on PATH, and returns the `bmux` commands it ran, spaces
// written as underscores.
func runExtension(t *testing.T, file, driver string, env []string, ctx map[string]any, events [][2]any) []string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	dir := t.TempDir()
	source, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.mjs"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "reports")
	bmux := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(log) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bmux"), []byte(bmux), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "driver.mjs"), []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	ctxJSON, _ := json.Marshal(ctx)
	eventsJSON, _ := json.Marshal(events)
	cmd := exec.Command(node, "driver.mjs", string(ctxJSON), string(eventsJSON))
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + dir + ":/usr/bin:/bin"}, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(log)
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(data)), " ", "_"))
}

func installed(t *testing.T, name, file string, env Env) string {
	t.Helper()
	a, _ := Find(name)
	if err := a.Install(env); err != nil {
		t.Fatal(err)
	}
	if status, err := a.Status(env); status != Installed || err != nil {
		t.Fatalf("%s after install: %v %v", name, status, err)
	}
	return filepath.Join(a.ConfigDir(env), file)
}

const piFile = "extensions/bmux-agent-state.ts"

func TestPiExtensionReportsTurnsPromptsAndSession(t *testing.T) {
	file := installed(t, "pi", piFile, testEnv(t, "pi"))
	inPane := []string{"BMUX_SESSION=work"}
	ui := map[string]any{"hasUI": true, "sessionFile": "/s/one.jsonl"}
	got := runExtension(t, file, piDriver, inPane, ui, [][2]any{
		{"session_start", map[string]any{}},
		{"agent_start", map[string]any{}},
		{"ui_prompt_start", map[string]any{}},
		{"ui_prompt_end", map[string]any{}},
		{"agent_end", map[string]any{}}, // pi may still retry: not settled
		{"agent_settled", map[string]any{}},
	})
	resume := "_--_pi_--session_/s/one.jsonl"
	want := []string{"agent_idle_pi" + resume, "agent_working_pi" + resume, "agent_blocked_pi" + resume, "agent_working_pi" + resume, "agent_idle_pi" + resume}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports:\n%v\nwant\n%v", got, want)
	}
	if got := runExtension(t, file, piDriver, nil, ui, [][2]any{{"session_start", map[string]any{}}}); len(got) != 0 {
		t.Fatalf("reported outside a bmux pane: %v", got)
	}
	if got := runExtension(t, file, piDriver, inPane, map[string]any{"hasUI": false}, [][2]any{{"session_start", map[string]any{}}, {"agent_start", map[string]any{}}}); len(got) != 0 {
		t.Fatalf("reported without a UI: %v", got)
	}
}

func TestOmpExtensionWaitsForTheFinalEndAndApprovals(t *testing.T) {
	env := testEnv(t, "omp")
	file := installed(t, "omp", piFile, env)
	inPane := []string{"BMUX_SESSION=work"}
	ui := map[string]any{"hasUI": true, "sessionFile": "/s/two.jsonl"}
	got := runExtension(t, file, piDriver, inPane, ui, [][2]any{
		{"agent_start", map[string]any{}},
		{"tool_approval_requested", map[string]any{"toolName": "bash"}},
		{"tool_approval_resolved", map[string]any{}},
		{"agent_end", map[string]any{"willContinue": true}},
		{"agent_end", map[string]any{}},
	})
	resume := "_--_omp_--resume=/s/two.jsonl"
	want := []string{"agent_working_omp" + resume, "agent_blocked_omp" + resume, "agent_working_omp" + resume, "agent_idle_omp" + resume}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports:\n%v\nwant\n%v", got, want)
	}
	// An omp started from another omp's shell isn't the pane's agent.
	if got := runExtension(t, file, piDriver, append(inPane, "OMPCODE=1"), ui, [][2]any{{"agent_start", map[string]any{}}}); len(got) != 0 {
		t.Fatalf("nested omp reported: %v", got)
	}
}

func TestOpencodePluginCountsBusySessionsAndPendingRequests(t *testing.T) {
	file := installed(t, "opencode", "plugins/bmux-agent-state.js", testEnv(t, "opencode"))
	type props = map[string]any
	got := runExtension(t, file, opencodeDriver, []string{"BMUX_SESSION=work"}, nil, [][2]any{
		{"session.created", props{"info": props{"id": "ses_root"}}},
		{"session.created", props{"info": props{"id": "ses_sub", "parentID": "ses_root"}}},
		{"session.status", props{"sessionID": "ses_root", "status": props{"type": "busy"}}},
		{"session.status", props{"sessionID": "ses_sub", "status": props{"type": "busy"}}},
		{"permission.asked", props{"id": "per_1", "sessionID": "ses_sub"}},
		{"permission.replied", props{"sessionID": "ses_sub", "requestID": "per_1", "reply": "once"}},
		{"session.idle", props{"sessionID": "ses_root"}}, // the subagent is still busy
		{"session.status", props{"sessionID": "ses_sub", "status": props{"type": "idle"}}},
		{"message.updated", props{}},
	})
	resume := "_--_opencode_--session_ses_root"
	want := []string{"agent_working_opencode" + resume, "agent_blocked_opencode" + resume, "agent_working_opencode" + resume, "agent_idle_opencode" + resume}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reports:\n%v\nwant\n%v", got, want)
	}
}

func TestOmpRefusesToShareAnAgentDirectoryWithPi(t *testing.T) {
	env := testEnv(t, "omp", "pi")
	shared := filepath.Join(env.Home, "agents")
	env.Getenv = func(name string) string {
		if name == "PI_CODING_AGENT_DIR" {
			return shared
		}
		return ""
	}
	a, _ := Find("omp")
	if err := a.Install(env); err == nil || !strings.Contains(err.Error(), "share") {
		t.Fatalf("shared directory accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shared, "extensions")); !os.IsNotExist(err) {
		t.Fatal("refused install still wrote files")
	}
	// PI_CONFIG_DIR gives omp its own directory under the home directory.
	env.Getenv = func(name string) string {
		if name == "PI_CONFIG_DIR" {
			return ".omp-work"
		}
		return ""
	}
	if got := a.ConfigDir(env); got != filepath.Join(env.Home, ".omp-work", "agent") {
		t.Fatalf("omp directory %s", got)
	}
	if err := a.Install(env); err != nil {
		t.Fatal(err)
	}
	if err := a.Uninstall(env); err != nil {
		t.Fatal(err)
	}
	if status, _ := a.Status(env); status != Available {
		t.Fatalf("after uninstall: %v", status)
	}
}
