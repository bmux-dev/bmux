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
| omp (`omp`) | `extensions/bmux-agent-state.ts` and the bmux skill in `skills/bmux/SKILL.md`, in `$PI_CODING_AGENT_DIR`, `~/$PI_CONFIG_DIR/agent` or `~/.omp/agent` | working until a turn really ends, blocked on tool approvals and the ask tool, idle, and the session for `omp --resume`. Refuses to install where pi would load it too. |
| opencode (`opencode`) | `plugins/bmux-agent-state.js` in `$XDG_CONFIG_HOME/opencode` or `~/.config/opencode` | working while any session, subagents included, is busy, blocked on permission requests and questions, idle, and the top-level session for `opencode --session` |
| pi (`pi`) | `extensions/bmux-agent-state.ts` and the bmux skill in `skills/bmux/SKILL.md`, in `$PI_CODING_AGENT_DIR` or `~/.pi/agent` | working until pi settles (after retries and follow-ups), blocked on extension prompts and the ask tool, idle, and the session for `pi --session` |

Every file bmux writes starts with a `bmux-integration: NAME vN` line, which
is how `status` spots an outdated version; reinstalling updates it. Edited JSON
files keep every other setting, hook and key order; uninstalling removes only
bmux's entries and files. Hooks run `bmux integration hook NAME` and
extensions run `bmux agent`, so `bmux` must be on the agent's `PATH`; outside
a bmux pane they do nothing.
Restarting the agent picks up a new integration. Two agents in one directory
each resume their own session after a restart.
