# Roadmap

bmux aims to cover what people use tmux **plus its most popular plugins** for,
with everything built in and no plugin manager.

## Now, next, later

| | Version | Theme |
| --- | --- | --- |
| **Shipped** | v0.2.0 | [Agent sidebar](#already-built) and [restore across restarts and reboots](#restore-follow-ups) |
| **Now** | [v0.3](#v03-daily-driver) | Daily driver: replace tmux for a full working day |
| **Next** | [v0.4](#v04-windows-and-layouts) | Windows and layouts |
| | [v0.5](#v05-keyboard-scrollback) | Keyboard scrollback and search |
| | [v0.6](#v06-scripting) | Scripting and automation |
| **Later** | [v1.0](#v10-stable) | Stable: compatibility, quality, distribution |
| | [Ideas](#ideas) | Popups, hints, multi-client, notifications |

Sizes are rough: **S** is a day or less, **M** a few days, **L** a week or more.

## Principles

- **Batteries included.** If tmux needs a plugin for it, bmux builds it in.
- **Modeless by default.** Keyboard selection and search can exist, but copy mode
  is never required.
- **Server owns terminals, client owns the clipboard.**
- **Honest persistence.** Restores bring back layouts, directories and
  allowlisted commands. bmux never claims to revive arbitrary running processes
  after a reboot.
- **Opinionated defaults, then configuration.** Defaults should match a good
  tmux setup so most people never need a config file.

## Not planned

| | Why |
| --- | --- |
| Native Windows | macOS and Linux only. WSL may work but isn't tested. |
| Plugin manager | The goal is to make plugins unnecessary. Scripting ([v0.6](#v06-scripting)) covers custom glue. |
| tmux config compatibility | bmux won't parse `.tmux.conf`. A migration guide maps common settings instead. |
| Remote or browser sharing | Use SSH plus `bmux attach`. |
| Reviving processes after reboot | Not possible in general. See principles. |

---

## v0.3: daily driver

**Goal:** a tmux user can switch to bmux for a full day without missing anything basic.
**Done when:** the owner uses bmux instead of tmux for a week of real work.

Everything planned for v0.3 is built; see [Already built](#already-built).
It ships once the owner has used bmux instead of tmux for a week of real work.
Fixes that week turns up go here.

## Restore follow-ups

v0.2.0 replaced tmux-resurrect and tmux-continuum: a versioned snapshot
(sessions, split tree, focus, zoom, per-pane cwd), saves every 5 seconds and on
clean shutdown, restore on server start, agent resume (reported commands or
each agent's "continue latest"), and `bmux restart`, which survives protocol
changes and reconnects clients. With an [agent integration](#first-run-and-agent-integrations),
two agents in one directory each resume their own session. Still to do:

| Item | Size | Notes |
| --- | --- | --- |
| Relaunch allowlist | M | e.g. `nvim -S`, `less`, `man`, `ssh host`. Everything else restores as a shell. |
| Optional scrollback in snapshots | M | Like resurrect's `capture-pane-contents`. Off by default; has a size cap. |
| Mark restored sessions | S | In the picker and sidebar. |

## v0.4: windows and layouts

**Goal:** organize a project the way tmux users do, with several windows per session.
**Done when:** a session can hold multiple windows, each with its own split
layout, and they survive save/restore.

| Item | Size | Notes |
| --- | --- | --- |
| Windows (tabs) per session | L | New, close, rename, next/previous, select by number, last window. Window list in the status bar. |
| Swap and rotate panes | S | tmux's `swap-pane`, `rotate-window`. |
| Break and join panes | M | Pane to new window, and back. |
| Preset layouts | M | Even-horizontal, even-vertical, main-horizontal, main-vertical, tiled. A key to cycle them and one to equalize. |
| Last pane toggle | S | |
| Project picker | M | Choose a project folder (zoxide or configured roots) and land in its session, creating it if needed. Replaces sesh and tmux-sessionizer. |
| Layout files | M | Declare a project's windows, panes and commands once. Replaces tmuxinator and tmuxp. Shares the session snapshot format. |

## v0.5: keyboard scrollback

**Goal:** find and copy anything in history without a mouse, and without a mandatory mode.
**Done when:** you can search, select and copy from the keyboard, and the mouse workflow is unchanged.

| Item | Size | Notes |
| --- | --- | --- |
| Search history (`/`, `?`, `n`/`N`) | M | Highlights matches. |
| Keyboard selection | M | vi-style `v`/`V`/Ctrl-v and `y`. |
| Jump between shell prompts | S | Uses OSC 133 prompt marks. |
| Auto-scroll while dragging a selection | S | |
| Reflow on resize | L | Wrapped lines rewrap, and copying them doesn't add newlines. |
| Scrollback size setting | S | 10,000 lines per pane today. |

## v0.6: scripting

**Goal:** dotfiles and scripts can drive bmux the way they drive tmux.
**Done when:** common tmux snippets (send a command to a pane, capture output,
open a pane in a project folder) have direct bmux equivalents.

Targets depend on windows from v0.4.

| Item | Size | Notes |
| --- | --- | --- |
| Targets for every command | M | `session:window.pane`, like tmux. |
| `send-keys`, `capture-pane` | M | `bmux send` and `bmux capture` cover sessions and panes; window targets come with v0.4. |
| `list-panes`, `list-windows`, `display-message` | S | |
| Format variables | M | `#{session_name}`, `#{pane_current_path}`, `#{pane_id}`, … |
| Hooks | M | `session-created`, `pane-exited`, `client-attached`, … |
| `run-shell`, `if-shell` | S | |
| Log a pane to a file | S | Like tmux's `pipe-pane` and the tmux-logging plugin. |

## v1.0: stable

**Goal:** safe to recommend to anyone.
**Done when:** everything below is in place and nothing in a release breaks
existing sessions without warning.

| Area | Items |
| --- | --- |
| Compatibility promise | Stable CLI, config format and snapshot format. Protocol changes never silently cost sessions (builds on `bmux restart`). |
| Terminal support | Extended keys (CSI u / kitty keyboard), hyperlinks (OSC 8), activity alerts, opt-in graphics passthrough. |
| Quality | vttest/esctest conformance runs, fuzzing for the protocol and emulator input, memory caps for large scrollback, latency and memory comparison with tmux. |
| Linux | Daily interactive use on Linux, not only CI. Wayland and X11 clipboard checked. |
| Distribution | Prebuilt macOS/Linux binaries (GoReleaser), Homebrew tap, checksums. |
| Docs | Demo GIF, tmux migration guide (config and plugin equivalents), CHANGELOG. |
| Project | CONTRIBUTING.md, issue and PR templates. |

## Ideas

Worth doing, not yet scheduled. Open an issue if one matters to you.

| Idea | Replaces |
| --- | --- |
| Floating popups for lazygit, fzf and scratch shells | `display-popup`, tmux-floax |
| Menus | `display-menu` |
| Hint mode: label URLs, paths, hashes and IPs on screen, copy or open with one key | tmux-thumbs, tmux-fingers, tmux-open, extrakto |
| Desktop notifications when a job or AI agent finishes (forward OSC 9/777 to the outer terminal); agents already get a sound | tmux-notify |
| Theme presets and custom status segments (host, time, prefix indicator) | tmux-power, tmux-prefix-highlight |
| Per-client window size and focus | `window-size`, `aggressive-resize` |
| Read-only attach, detach other clients | `attach -r`, `detach -a` |
| Type into every pane at once | `synchronize-panes` |
| Lock a session | `lock-session` |

## Already built

| tmux / plugin | bmux |
| --- | --- |
| Sessions, detach/attach, `new`/`list`/`attach`/`rename`/`kill`, `kill-pane` (Ctrl-a x), refusing to nest | Built in |
| tmux-sessionx / tmux-fzf session picker | Session picker with fuzzy filter and live preview |
| tmux-sessionist | Create, rename and terminate from the picker; Ctrl-a n, Ctrl-a p last session, Ctrl-a ( / ) previous/next; bare `bmux` names sessions after the directory |
| tmux-resurrect + tmux-continuum | Save every 5 seconds and on shutdown, restore on server start, agent resume, `bmux restart` |
| herdr-style agent overview | Sidebar of sessions and detected agents with working/waiting state; bell, OSC 9/777 and `bmux agent` reports mark panes that need you |
| herdr's first run and agent integrations | A welcome screen on first attach leading into the integrations panel (Ctrl-a i); `bmux integration` for Claude Code, Codex, omp, opencode and pi, reporting exact working/blocked/idle and the session to resume |
| Agents supervising agents | `bmux agents`, `bmux capture`, `bmux send --if-seq`, `bmux wait-agent`; `bmux guide`, `bmux capabilities` and an installed skill teach agents to use them |
| tmux-pain-control, `split-window -c "#{pane_current_path}"`, `select-pane`, `resize-pane` (repeatable), zoom | `Ctrl-a \|` / `-` in the focused pane's directory, Ctrl-h/j/k/l, `Ctrl-a` arrows / `o`, `Ctrl-a h/j/k/l`, `Ctrl-a m` |
| `focus-events on`, vim-tmux-focus-events | Focus in/out (`?1004`) for the focused pane while a client's terminal has focus |
| `set-titles on` | Window title from the session and the focused pane's title; `ui.set-title` turns it off |
| vim-tmux-navigator | Vim/Neovim-aware Ctrl-h/j/k/l, edge-crossing via [an editor mapping](docs/editor-navigation.md), `keys.pass-through` for other programs (its `is_vim` pattern) |
| tmux-yank, `set-clipboard` | Copy on mouse release: pbcopy, wl-copy, xclip, xsel or OSC 52; apps' OSC 52 writes (Neovim yanks) copied by every attached client |
| tmux-better-mouse-mode, `mouse on` | Click to focus, drag dividers to resize, drag/double/triple-click to select, wheel to scroll; Alt overrides mouse-aware apps |
| tmux-notify (agents) | Sound when a background agent becomes ready or blocked |
| tmux-prefix-highlight | Status line shows prefix keys while Ctrl-a is pending |
| `.tmux.conf`, `source-file` | `~/.config/bmux/config.toml` (Helix layout), reload with Ctrl-a r, `bmux reload` or SIGUSR1; `bmux default-config` |
| `bind-key`, `unbind-key`, `list-keys` (`C-b ?`) | `[keys.root]` and `[keys.prefix]` in the config file; Ctrl-a ? lists the bindings in effect |
| Status themes | Powerline status bars, accent colors, light theme |

v0.1.1 fixed palette colors ignoring the terminal theme, `bmux focus` after a
rename, directional navigation while zoomed, and `bmux --help` wording.

v0.2.0 added the agent sidebar, restore across restarts and reboots with
`bmux restart`, flicker-free synchronized output, and app backgrounds set with
OSC 11.

Since v0.2.0, unreleased (v0.3): splits in the focused pane's directory,
dragging dividers, rebindable keys with a key list, Ctrl-h/j/k/l pass-through,
focus events, outer window titles, apps' OSC 52 copies, the welcome screen and
agent integrations.
