---
title: "Agent integrations"
description: "An integration makes an agent report its own state and exact session instead of relying on output timing."
sidebar:
  order: 2
---

Output timing can't tell a finished turn from a permission prompt. An
integration makes the agent report its own state and exact session instead.
Integrations change the agent's own config files, so they're installed only
when you ask. **Ctrl-a i** opens the integrations panel: one row per agent,
marked ✓ installed, ↻ update available, + available (the agent is on your
`PATH`) or – not found. **Enter** installs every available or outdated one
and shows what happened; **Esc** closes it. The same from a shell:

```sh
bmux integration status            # installed, update available, available or not found
bmux integration install           # every agent on PATH, and updates
bmux integration install claude    # or name them
bmux integration uninstall claude
```

| Agent | What it installs | Reports |
| --- | --- | --- |
| Claude Code (`claude`) | `hooks/bmux-agent-state.sh` and hook entries in `settings.json`, plus the [bmux skill](/bmux/agents/scripting/) in `skills/bmux/SKILL.md`, in `$CLAUDE_CONFIG_DIR` or `~/.claude` | working, blocked on permission prompts and MCP questions, idle, and the session for `claude --resume` |
| Codex (`codex`) | `hooks/bmux-agent-state.sh` and hook entries in `hooks.json`, in `$CODEX_HOME` or `~/.codex` | working, blocked on approvals, idle, and the session for `codex resume`. Codex runs new hooks only after you trust them in `/hooks`. |
| omp (`omp`) | `extensions/bmux-agent-state.ts` and the bmux skill in `skills/bmux/SKILL.md`, in `$PI_CODING_AGENT_DIR`, `~/$PI_CONFIG_DIR/agent` or `~/.omp/agent` | working until a turn really ends, blocked on tool approvals, the ask tool and extensions' own prompts (see below), idle, and the session for `omp --resume`. Refuses to install where pi would load it too. |
| opencode (`opencode`) | `plugins/bmux-agent-state.js` in `$XDG_CONFIG_HOME/opencode` or `~/.config/opencode` | working while the bound session or its children are busy, blocked on permissions and forms/questions, idle, and the root session for resuming; V2 requires the standalone launch below |
| pi (`pi`) | `extensions/bmux-agent-state.ts` and the bmux skill in `skills/bmux/SKILL.md`, in `$PI_CODING_AGENT_DIR` or `~/.pi/agent` | working until pi settles (after retries and follow-ups), blocked on extension prompts, the ask tool and extensions' own prompts (see below), idle, and the session for `pi --session` |

An omp or pi extension that asks you something with its own dialog (such as
`ctx.ui.confirm`) can mark the pane blocked: emit `agent:blocked` on the
shared event bus, `pi.events.emit("agent:blocked", { active: true, label })`,
before the dialog and `{ active: false }` after it, in a `finally`. Each
`active: true` needs its own `active: false`, and the end of the turn clears
any left over.

Every file bmux writes starts with a `bmux-integration: NAME vN` line, which
is how `status` spots an outdated version; reinstalling updates it. Edited JSON
files keep every other setting, hook and key order; uninstalling removes only
bmux's entries and files. Hooks run `bmux integration hook NAME` and
extensions run `bmux agent`, so `bmux` must be on the agent's `PATH`; outside
a bmux pane they do nothing.
Restarting the agent picks up a new integration. OpenCode V2 watches its plugin
directory and reloads changes automatically; do not restart the shared service
just to update this file. Two agents in one directory each resume their own
session after a restart.

## OpenCode V2

The plugin supports OpenCode V2 (validated with 2.1.4) and the dual-entrypoint
V1 plugin API in **1.18.29 and newer**. Older V1 releases are not supported by
this integration version. The header's `v2` is the **bmux integration version**,
not the OpenCode version. An older bmux-generated plugin shows **update available**:

```sh
bmux integration install opencode
```

For exact reporting on V2, start a **private server** in each bmux pane and
explicitly bind its root conversation. For a new conversation, run this from
the pane's project directory:

```sh
bmux_opencode_session="ses_bmux_$(date +%s)_$$"
BMUX_OPENCODE_SESSION="$bmux_opencode_session" \
  opencode --standalone --session "$bmux_opencode_session"
```

To continue a conversation, use its existing `ses...` ID in both places.
The ID must contain only letters, digits and underscores. The plugin reports
only that session and its descendants in the same location; it ignores other
roots, other projects and inaccessible sessions. If you switch to a different
root conversation or move the session to another directory, relaunch there
with the new binding. Its saved resume command preserves the binding and
`--standalone`, so bmux restores a **new process** on that conversation.
State counts come from live events, not a replay of in-flight work. If a plugin
update/reload happens during a turn, finish that turn and relaunch the private
client before relying on exact counts; existing waits are not reconstructed.

**Shared-service pane reporting is not supported by this fix.** The normal V2
background service does not inherit the environment of each terminal client.
Neither a plugin's project directory nor a shared server's `BMUX_SESSION`
identifies the pane viewing an arbitrary conversation. Without the explicit
binding the V2 plugin loads but reports nothing; bmux still recognizes the
foreground OpenCode client and falls back to output timing. Do not export one
pane's binding into the shared service, or use it for remote clients. Accurate
shared-service support needs a separate client-to-session binding mechanism.

V1 retains its pane-local server event handling and conversation resume command.
Uninstalling removes only bmux's managed plugin, leaving other plugins and
`opencode.json(c)` unchanged. Installation refuses to overwrite an unmanaged
file at the same path.
