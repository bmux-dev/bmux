package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// piDriver loads a pi/omp extension with a stand-in for the extension API
// (pi.on and the shared pi.events bus) and sends it [name, event] pairs with
// the given ctx.
const piDriver = `import extension from "./extension.mjs";
const handlers = {};
const on = (name, fn) => { handlers[name] = fn; };
extension({ on, events: { on } });
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
const opencodeDriver = `import plugin from "./extension.mjs";
const hooks = await plugin.server({});
for (const [type, properties] of JSON.parse(process.argv[3])) {
  await hooks.event?.({ event: { type, properties } });
  await new Promise((resolve) => setTimeout(resolve, 60));
}
`

// V2 fixtures follow https://opencode.ai/v2/openapi.json: data, not properties,
// and form.created wraps Form.Info. A live subscription must abort on unload.
const opencodeV2Driver = `import plugin from "./extension.mjs";
import assert from "node:assert/strict";
const input = JSON.parse(process.argv[2]);
const events = JSON.parse(process.argv[3]);
assert.equal(plugin.id, "bmux.agent-state");
assert.equal(typeof plugin.setup, "function");
let subscribed = false;
let completed = false;
let signal;
const cleanup = plugin.setup({
  location: { directory: input.directory ?? "/work" },
  session: { get: async ({ sessionID }) => {
    const info = input.sessions?.[sessionID];
    if (!info) throw new Error("session inaccessible");
    return { id: sessionID, location: { directory: "/work" }, ...info };
  } },
  event: { subscribe: (options) => {
    subscribed = true;
    signal = options.signal;
    assert(options.types.includes("form.created"));
    assert(!options.types.includes("question.asked"));
    return (async function* () {
      for (const [type, data] of events) {
        if (signal.aborted) break;
        yield { type, data };
        await new Promise(resolve => setTimeout(resolve, 60));
      }
      await new Promise(resolve => {
        if (signal.aborted) resolve();
        else signal.addEventListener("abort", resolve, { once: true });
      });
      completed = true;
    })();
  } },
});
if (cleanup) {
  assert(subscribed);
  await new Promise(resolve => setTimeout(resolve, events.length * 90 + 100));
  cleanup();
  assert(signal.aborted);
  await new Promise(resolve => setTimeout(resolve, 100));
  assert(completed);
} else assert(!subscribed);
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
	if ctx["reportEnv"] == true {
		bmux = "#!/bin/sh\nprintf '%s\\n' \"$BMUX_SESSION:$BMUX_SESSION_ID:$BMUX_PANE $*\" >> " + shellQuote(log) + "\n"
	}
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
		{"agent:blocked", map[string]any{"active": true, "label": "approve path access"}}, // another extension's prompt
		{"agent:blocked", map[string]any{"active": false}},
		{"agent:blocked", map[string]any{"active": true}}, // an unanswered prompt ends with the turn
		{"agent_end", map[string]any{}},
	})
	resume := "_--_omp_--resume=/s/two.jsonl"
	want := []string{"agent_working_omp" + resume, "agent_blocked_omp" + resume, "agent_working_omp" + resume, "agent_blocked_omp" + resume, "agent_working_omp" + resume, "agent_blocked_omp" + resume, "agent_idle_omp" + resume}
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

func TestOpencodeV2ReportsOnlyBoundSessionTree(t *testing.T) {
	file := installed(t, "opencode", "plugins/bmux-agent-state.js", testEnv(t, "opencode"))
	type props = map[string]any
	ctx := props{"sessions": props{
		"ses_root":    props{},
		"ses_sub":     props{"parentID": "ses_root"},
		"ses_nested":  props{"parentID": "ses_sub"},
		"ses_other":   props{}, // even another root in the same directory is unrelated
		"ses_foreign": props{"location": props{"directory": "/other"}},
		"ses_cycle":   props{"parentID": "ses_cycle"},
	}}
	events := [][2]any{
		{"session.execution.started", props{"sessionID": "ses_other"}},
		{"permission.asked", props{"id": "unrelated", "sessionID": "ses_foreign"}},
		{"session.execution.started", props{"sessionID": "ses_missing"}},
		{"session.execution.started", props{"sessionID": "ses_cycle"}},
		{"session.execution.started", props{"sessionID": "ses_root"}},
		{"session.status", props{"sessionID": "ses_sub", "status": props{"type": "retry"}}},
		{"permission.asked", props{"id": "per_1", "sessionID": "ses_sub"}},
		{"form.created", props{"form": props{"id": "frm_1", "sessionID": "ses_nested"}}},
		{"permission.replied", props{"requestID": "per_1", "sessionID": "ses_sub"}},
		{"form.replied", props{"id": "frm_1", "sessionID": "ses_nested"}},
		{"session.execution.succeeded", props{"sessionID": "ses_root"}},
		{"session.execution.failed", props{"sessionID": "ses_sub"}},
		{"session.execution.started", props{"sessionID": "ses_root"}},
		{"form.created", props{"form": props{"id": "frm_2", "sessionID": "ses_root"}}},
		{"form.cancelled", props{"id": "frm_2", "sessionID": "ses_root"}},
		{"permission.asked", props{"id": "per_2", "sessionID": "ses_root"}},
		{"session.execution.interrupted", props{"sessionID": "ses_root"}}, // clears unanswered requests
		{"session.status", props{"sessionID": "ses_root", "status": props{"type": "busy"}}},
		{"session.status", props{"sessionID": "ses_root", "status": props{"type": "idle"}}},
		{"session.execution.started", props{"sessionID": "ses_root"}},
		{"session.idle", props{"sessionID": "ses_root"}},
	}
	resume := "_--_env_BMUX_OPENCODE_SESSION=ses_root_opencode_--standalone_--session_ses_root"
	wantStates := []string{"working", "blocked", "working", "idle", "working", "blocked", "working", "blocked", "idle", "working", "idle", "working", "idle"}
	var want []string
	for _, state := range wantStates {
		want = append(want, "agent_"+state+"_opencode"+resume)
	}
	env := []string{"BMUX_SESSION=work", "BMUX_SESSION_ID=3", "BMUX_PANE=2", "BMUX_OPENCODE_SESSION=ses_root"}
	if got := runExtension(t, file, opencodeV2Driver, env, ctx, events); !reflect.DeepEqual(got, want) {
		t.Fatalf("reports:\n%v\nwant\n%v", got, want)
	}
	// A plugin instance in another location must not claim this tree.
	ctx["directory"] = "/other"
	if got := runExtension(t, file, opencodeV2Driver, env, ctx, events); len(got) != 0 {
		t.Fatalf("foreign plugin instance reported: %v", got)
	}
}

func TestOpencodeV2DoesNotInferPaneFromSharedServerEnvironment(t *testing.T) {
	file := installed(t, "opencode", "plugins/bmux-agent-state.js", testEnv(t, "opencode"))
	for _, env := range [][]string{
		nil,
		{"BMUX_SESSION=work", "BMUX_PANE=1"}, // service once started in a pane
		{"BMUX_OPENCODE_SESSION=ses_root"},   // no terminal pane
		{"BMUX_SESSION=work", "BMUX_SESSION_ID=3", "BMUX_PANE=1", "BMUX_OPENCODE_SESSION=not-a-session"},
		{"BMUX_SESSION=work", "BMUX_SESSION_ID=3", "BMUX_PANE=bad", "BMUX_OPENCODE_SESSION=ses_root"},
	} {
		if got := runExtension(t, file, opencodeV2Driver, env, map[string]any{}, nil); len(got) != 0 {
			t.Fatalf("unbound server reported: %v", got)
		}
	}
}

func TestOpencodeV2InstancesKeepTheirOwnQueuesAndPaneTargets(t *testing.T) {
	file := installed(t, "opencode", "plugins/bmux-agent-state.js", testEnv(t, "opencode"))
	driver := `import plugin from "./extension.mjs";
const cleanups = [];
for (const pane of [1, 2]) {
  process.env.BMUX_PANE = String(pane);
  process.env.BMUX_OPENCODE_SESSION = "ses_" + pane;
  const sessionID = process.env.BMUX_OPENCODE_SESSION;
  cleanups.push(plugin.setup({
    location: { directory: "/work" },
    session: { get: async () => ({ id: sessionID, location: { directory: "/work" } }) },
    event: { subscribe: async function* ({ signal }) {
      yield { type: "session.execution.started", data: { sessionID } };
      yield { type: "session.execution.succeeded", data: { sessionID } };
      await new Promise(resolve => signal.addEventListener("abort", resolve, { once: true }));
    } },
  }));
}
// A rename does not change the numeric session ID captured by each reporter.
process.env.BMUX_SESSION = "renamed";
process.env.BMUX_SESSION_ID = "999";
process.env.BMUX_PANE = "999";
await new Promise(resolve => setTimeout(resolve, 300));
for (const cleanup of cleanups) cleanup();
`
	env := []string{"BMUX_SESSION=work", "BMUX_SESSION_ID=3"}
	got := runExtension(t, file, driver, env, map[string]any{"reportEnv": true}, nil)
	want := []string{}
	for _, pane := range []string{"1", "2"} {
		for _, state := range []string{"working", "idle"} {
			want = append(want, "work:3:"+pane+"_agent_"+state+"_opencode_--_env_BMUX_OPENCODE_SESSION=ses_"+pane+"_opencode_--standalone_--session_ses_"+pane)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("instance reports:\n%v\nwant\n%v", got, want)
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
