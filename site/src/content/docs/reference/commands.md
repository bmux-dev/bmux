---
title: "Commands"
description: "Every bmux command and what it does."
sidebar:
  order: 1
---

| Command | Behavior |
| --- | --- |
| `bmux` | Create and attach to a fresh session in the current directory; `bmux attach` returns to an existing one |
| `bmux new [--dir PATH] [--detach] NAME` | Create a named session; attach unless `--detach` is set |
| `bmux attach [NAME]` | Attach by name, or to the most recently used session (the first in `bmux list`) |
| `bmux project [DIR]` | Attach to the session started in `DIR`, creating one named after it if there is none; without `DIR`, open the [new-session popup](/bmux/guides/project-picker/) |
| `bmux layout save [SESSION]` | Write the session's windows, splits, pane directories and agents' resume commands to `.bmux.toml` in its folder, and trust it; inside a pane, the pane's session |
| `bmux layout trust [DIR]` | Let `DIR`'s `.bmux.toml` (default: the current folder) run its commands, as it is now |
| `bmux list` | Show sessions, state, window/pane/client counts, and starting directories, most recently used first |
| `bmux rename OLD NEW` | Rename a session |
| `bmux kill NAME` | Terminate the session and all its panes, without a confirmation prompt |
| `bmux focus DIRECTION` | Move from the calling pane toward `left`, `right`, `up`, or `down`; used by editor mappings |
| `bmux zoomed` | Print `true` if the calling pane's window is zoomed, otherwise `false`; used by editor mappings to stay in a zoomed pane |
| `bmux agent STATE [NAME] [-- COMMAND...]` | Report the calling pane's agent as `working`, `blocked`, `idle`, or `clear`, optionally with the command that resumes its conversation; used by agent hooks |
| `bmux agents [--json]` | List panes running a coding agent as `SESSION:PANE` targets, with state and [seq](/bmux/agents/scripting/) |
| `bmux capture [--json] TARGET` | Print a pane's visible text; `TARGET` is `SESSION`, `SESSION:PANE`, `SESSION:WINDOW.` or `SESSION:WINDOW.N` (see [targets](/bmux/agents/scripting/)) |
| `bmux send TARGET [--if-seq N] (--key KEY \| --text TEXT)...` | Type keys and text into one pane, in order; with `--if-seq`, only if its agent hasn't changed state |
| `bmux wait-agent [--state S,...] [--since SEQ] [--timeout D \| --notify SESSION:PANE] [TARGET...]` | Wait until another agent pane is `blocked` or `idle` and print it as JSON; with `--notify`, keep running and type each change into a supervising agent's pane while it's idle |
| `bmux split [TARGET] [-h \| -v] [-c DIR] [-d] [-- COMMAND...]` | Split a pane side by side (`-h`) or stacked (`-v`, the default), in its directory or `DIR`, type `COMMAND` into the new shell and print the new pane's target; `-d` keeps focus where it was. Without `TARGET`, the calling pane |
| `bmux new-window [TARGET] [-n NAME] [-c DIR] [-d] [-- COMMAND...]` | Open a window in the target's session, with the same options as `split`; prints the new pane |
| `bmux select TARGET` | Focus a pane, bringing its window forward, or select a window (`SESSION:WINDOW.`) |
| `bmux kill-pane TARGET`, `bmux kill-window TARGET` | Close a pane, or the target's window, and their programs without asking |
| `bmux resize [TARGET] left\|right\|up\|down [CELLS]` | Move a pane's divider, 5 cells unless `CELLS` says |
| `bmux zoom [TARGET]` | Zoom or unzoom a pane |
| `bmux rename-window [TARGET] NAME` | Rename the target's window; an empty name shows its focused pane's title |
| `bmux guide` | Print the guide for coding agents: how to read, drive and watch panes |
| `bmux capabilities` | Print the version, protocol version, commands and calling pane as JSON |
| `bmux integration status [AGENT]` | List agent integrations: installed, update available, available (the agent is on `PATH`) or not found |
| `bmux integration install [AGENT...]` | Install or update integrations; with no names, every one that is available or out of date |
| `bmux integration uninstall AGENT...` | Remove what bmux installed for these agents |
| `bmux restart` | Save sessions, stop the server and start this binary's server, which restores them; attached clients reconnect |
| `bmux update [--check]` | Install the newest release after checking it against its checksums; `--check` only reports. See [Updates](/bmux/start/installation/#updates) |
| `bmux version`, `bmux --version` | Print the version |
| `bmux help`, `bmux --help`, `bmux -h` | Show help |

Put `new` options **before** the name. `--dir` defaults to the current directory;
the directory must exist. Relative paths are resolved from the invoking shell.
`--detach` defaults to false. Use `--` before a name beginning with a dash.

```sh
bmux new --dir ~/projects/api api
bmux new --detach logs
bmux list
bmux attach api
bmux rename api backend
bmux kill logs
```

`bmux attach` does not create a session when none exist. Start one with `bmux`
or `bmux new NAME`. The server uses `bmux --serve` internally; normal use does
not require launching it yourself.

Inside a bmux pane, `bmux`, `bmux attach` and `bmux new NAME` refuse to start a
second client, like tmux checks `$TMUX`: a nested client would shrink into the
pane and fight over keys. Switch with Ctrl-a s instead, create sessions with
`bmux new --detach NAME`, or unset `BMUX_SESSION` to nest anyway.
