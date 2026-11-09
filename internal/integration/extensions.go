package integration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// fileAgent is an integration that is one managed file the agent loads, such
// as a pi or omp extension or an opencode plugin.
type fileAgent struct {
	name    string
	version int
	file    string // relative to the config dir
	text    func(header string) string
	// check refuses an install that would misfire, when set.
	check func(Env) error
}

func (f fileAgent) agent(label, command string, configDir func(Env) string, notes string) Agent {
	path := func(dir string) string { return filepath.Join(dir, f.file) }
	return Agent{
		Name: f.name, Label: label, Command: command, Version: f.version, Notes: notes,
		configDir: configDir, check: f.check,
		install: func(dir string) error {
			return writeFile(path(dir), []byte(f.text(headerLines("//", f.name, f.version))), 0o644)
		},
		uninstall: func(dir string) error { return removeFile(path(dir), f.name) },
		installed: func(dir string) (int, error) { return fileVersion(path(dir), f.name) },
	}
}

// piExtension is the extension for pi and for omp, which share pi's
// extension API. It reports through `bmux agent` and does nothing outside a
// bmux pane or without a UI (print and RPC modes).
//
//   - working from agent_start until the agent settles (pi's agent_settled;
//     omp's agent_end unless it will continue on its own)
//   - blocked while an extension prompt (pi's ui_prompt_*), a tool approval
//     (omp's tool_approval_*), the ask tool or another extension's prompt
//     (agent:blocked on the shared pi.events bus: {active, label?}) waits on you
//   - the session file, for resuming exactly this session
//
// Leaving the foreground already drops a recognized agent's reports, so it
// never needs to report clear.
func piExtension(agent, settle, skipWhen, resume string) func(string) string {
	return func(header string) string {
		return header + `// @ts-nocheck
import { execFile } from "node:child_process";
import path from "node:path";

const AGENT = "` + agent + `";
const enabled = !!process.env.BMUX_SESSION` + skipWhen + `;

let queue = Promise.resolve();
let last = "";
let sessionFile;

function report(state) {
  const args = ["agent", state, AGENT];
  if (sessionFile) args.push("--", ` + resume + `);
  const key = args.join("\0");
  if (key === last) return;
  last = key;
  // One at a time, in order; failures (no server, no bmux) are ignored.
  queue = queue.then(() => new Promise((resolve) => {
    execFile("bmux", args, { timeout: 3000, windowsHide: true }, () => resolve());
  }));
}

function remember(ctx) {
  try {
    const file = ctx?.sessionManager?.getSessionFile?.();
    if (typeof file === "string" && path.isAbsolute(file)) sessionFile = file;
  } catch {}
}

export default function (pi) {
  if (!enabled) return;
  let interactive = false;
  let running = false;
  let waits = 0;
  const publish = () => {
    if (interactive) report(waits > 0 ? "blocked" : running ? "working" : "idle");
  };
  const wait = (delta) => {
    waits = Math.max(0, waits + delta);
    publish();
  };
  const start = (ctx) => {
    interactive = ctx?.hasUI !== false;
    remember(ctx);
  };

  pi.on("session_start", (_event, ctx) => {
    start(ctx);
    running = ctx?.isIdle?.() === false;
    publish();
  });
  pi.on("session_switch", (_event, ctx) => {
    start(ctx);
    running = false;
    waits = 0;
    publish();
  });
  pi.on("agent_start", (_event, ctx) => {
    // Also after a reload mid-run, which skips session_start.
    start(ctx);
    running = true;
    publish();
  });
  pi.on("` + settle + `", (event) => {
    if (event?.willContinue === true) return;
    running = false;
    waits = 0;
    publish();
  });
  pi.on("ui_prompt_start", () => wait(1));
  pi.on("ui_prompt_end", () => wait(-1));
  pi.on("tool_approval_requested", () => wait(1));
  pi.on("tool_approval_resolved", () => wait(-1));
  // Other extensions can't emit the events above; they mark their own prompts
  // on the shared bus instead. The bus is not part of pi.on, so it's optional.
  pi.events?.on?.("agent:blocked", (data) => wait(data?.active === true ? 1 : -1));
  pi.on("tool_execution_start", (event) => {
    if (event?.toolName === "ask") wait(1);
  });
  pi.on("tool_execution_end", (event) => {
    if (event?.toolName === "ask") wait(-1);
  });
}
`
	}
}

func piDir(e Env) string { return dirFromEnv("PI_CODING_AGENT_DIR", ".pi", "agent")(e) }

// ompDir follows omp: PI_CODING_AGENT_DIR is the whole agent directory, else
// PI_CONFIG_DIR names one under the home directory, else ~/.omp/agent.
func ompDir(e Env) string {
	if dir := e.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return dir
	}
	if dir := e.Getenv("PI_CONFIG_DIR"); dir != "" {
		return filepath.Join(e.Home, dir, "agent")
	}
	return filepath.Join(e.Home, ".omp", "agent")
}

var pi = fileAgent{
	name: "pi", version: 3, file: filepath.Join("extensions", "bmux-agent-state.ts"),
	text: piExtension("pi", "agent_settled", "", `"pi", "--session", sessionFile`),
}

var omp = fileAgent{
	name: "omp", version: 3, file: filepath.Join("extensions", "bmux-agent-state.ts"),
	// omp marks the shells it spawns with OMPCODE=1; an omp started from one
	// is nested, not the pane's agent.
	text: piExtension("omp", "agent_end", ` && process.env.OMPCODE !== "1"`, `"omp", "--resume=" + sessionFile`),
	check: func(e Env) error {
		if filepath.Clean(ompDir(e)) == filepath.Clean(piDir(e)) {
			return fmt.Errorf("pi and omp share %s, so each would load the other's extension; give them separate agent directories first", ompDir(e))
		}
		return nil
	},
}

// opencodePlugin has separate V1 and V2 entrypoints. V2 requires an explicit
// root session binding: a shared service's environment is not its clients'.
func opencodePlugin(header string) string {
	return header + `import { execFile } from "node:child_process";
import path from "node:path";

// State and subprocess queues belong to an instance, not the imported module.
function reporter(resume) {
  let queue = Promise.resolve();
  let last = "";
  let stopped = false;
  const controller = new AbortController();
  const env = { ...process.env }; // retain the pane ID across session renames
  return {
    report(state) {
      const args = ["agent", state, "opencode", ...resume()];
      const key = args.join("\0");
      if (stopped || key === last) return;
      last = key;
      queue = queue.then(() => new Promise((resolve) => {
        if (stopped) return resolve();
        execFile("bmux", args, { env, signal: controller.signal, timeout: 3000, windowsHide: true }, () => resolve());
      }));
    },
    stop() { stopped = true; controller.abort(); },
  };
}

// A plain definition avoids an npm dependency for this generated JS file.
// V2 consumes setup; V1 >= 1.18.29 consumes server (the documented dual API).
export default {
  id: "bmux.agent-state",
  setup(ctx) {
    const root = process.env.BMUX_OPENCODE_SESSION;
    if (!process.env.BMUX_SESSION || !/^[1-9][0-9]*$/.test(process.env.BMUX_PANE ?? "") ||
        !/^[1-9][0-9]*$/.test(process.env.BMUX_SESSION_ID ?? "") ||
        !/^ses[a-zA-Z0-9_]+$/.test(root ?? "")) return;
    const directory = ctx.location.directory;
    const controller = new AbortController();
    const output = reporter(() => ["--", "env", "BMUX_OPENCODE_SESSION=" + root,
      "opencode", "--standalone", "--session", root]);
    const busy = new Set();
    const waiting = new Map();
    const belongs = async (sessionID) => {
      // Fetch session context even after a reload, when creation was missed.
      // Do not infer pane ownership from plugin location or event location.
      const seen = new Set();
      while (sessionID && !seen.has(sessionID)) {
        seen.add(sessionID);
        const info = await ctx.session.get({ sessionID }, { signal: controller.signal });
        if (path.resolve(info.location.directory) !== path.resolve(directory)) return false;
        if (info.id === root) return true;
        sessionID = info.parentID;
      }
      return false;
    };
    const settle = (sessionID) => {
      busy.delete(sessionID);
      for (const [key, owner] of waiting) {
        if (owner === sessionID) waiting.delete(key);
      }
    };
    void (async () => {
      try {
        for await (const event of ctx.event.subscribe({ signal: controller.signal,
          types: ["session.status", "session.idle", "session.execution.started",
            "session.execution.succeeded", "session.execution.failed", "session.execution.interrupted",
            "permission.asked", "permission.replied", "form.created", "form.replied", "form.cancelled"] })) {
          try {
            const p = event.data;
            const sessionID = p.form?.sessionID ?? p.sessionID;
            if (!sessionID || !await belongs(sessionID) || controller.signal.aborted) continue;
            switch (event.type) {
              case "session.execution.started":
                busy.add(sessionID);
                break;
              case "session.status":
                if (p.status.type === "idle") settle(sessionID);
                else busy.add(sessionID); // busy or retry
                break;
              case "session.idle":
              case "session.execution.succeeded":
              case "session.execution.failed":
              case "session.execution.interrupted":
                settle(sessionID);
                break;
              case "permission.asked":
                waiting.set("permission:" + p.id, sessionID);
                break;
              case "permission.replied":
                waiting.delete("permission:" + p.requestID);
                break;
              case "form.created":
                waiting.set("form:" + p.form.id, sessionID);
                break;
              case "form.replied":
              case "form.cancelled":
                waiting.delete("form:" + p.id);
                break;
              default:
                continue;
            }
            output.report(waiting.size > 0 ? "blocked" : busy.size > 0 ? "working" : "idle");
          } catch {
            // Deleted/inaccessible sessions are unattributed; keep consuming.
          }
        }
      } catch {
        // Abort/unload and an unavailable event stream must not crash OpenCode.
      }
    })();
    return () => { controller.abort(); output.stop(); };
  },
  server: opencodeV1,
};

async function opencodeV1() {
  if (!process.env.BMUX_SESSION) return {};
  const children = new Set();
  const busy = new Set();
  const waiting = new Map(); // request id -> session id
  let current;
  const output = reporter(() => current ? ["--", "opencode", "--session", current] : []);
  return {
    dispose: () => output.stop(),
    event: async ({ event }) => {
      const p = event?.properties ?? {};
      switch (event?.type) {
        case "session.created":
        case "session.updated":
          if (p.info?.id && p.info.parentID) children.add(p.info.id);
          return;
        case "session.status":
          if (p.status?.type === "idle") busy.delete(p.sessionID);
          else busy.add(p.sessionID);
          if (p.sessionID && !children.has(p.sessionID)) current = p.sessionID;
          break;
        case "session.idle":
          busy.delete(p.sessionID);
          break;
        case "permission.asked":
        case "permission.updated":
        case "question.asked":
          waiting.set(p.id, p.sessionID);
          break;
        case "permission.replied":
        case "question.replied":
        case "question.rejected":
          waiting.delete(p.requestID ?? p.permissionID);
          break;
        default:
          return;
      }
      output.report(waiting.size > 0 ? "blocked" : busy.size > 0 ? "working" : "idle");
    },
  };
}
`
}

// opencodeDir is opencode's global config directory.
func opencodeDir(e Env) string {
	if dir := e.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opencode")
	}
	return filepath.Join(e.Home, ".config", "opencode")
}

var opencode = fileAgent{
	name: "opencode", version: 2, file: filepath.Join("plugins", "bmux-agent-state.js"),
	text: opencodePlugin,
	check: func(e Env) error {
		file := filepath.Join(opencodeDir(e), "plugins", "bmux-agent-state.js")
		if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if version, err := fileVersion(file, "opencode"); err != nil {
			return err
		} else if version == 0 {
			return fmt.Errorf("refusing to overwrite unmanaged OpenCode plugin %s; move it aside first", file)
		}
		return nil
	},
}
