# Roadmap

bmux aims to cover what people use tmux **plus its most popular plugins** for,
with everything built in and no plugin manager.

## Now, next, later

| | Version | Theme |
| --- | --- | --- |
| **Shipped** | v0.6.0 | Scripting and automation, images in panes ([already built](#already-built)) |
| **Now** | [v0.7](#v07-alerts-and-hardening) | Activity alerts, and everything 1.0 needs |
| **Later** | [v1.0](#v10-stable) | v0.7, once it's stable: no new features |
| | [Ideas](#ideas) | Popups, hints, multi-client, notifications |

Sizes are rough: **S** is a day or less, **M** a few days, **L** a week or more.

## Principles

- **Batteries included.** If tmux needs a plugin for it, bmux builds it in.
- **Modeless by default.** Keyboard selection and search can exist, but copy mode
  is never required.
- **Server owns terminals, client owns the clipboard.**
- **Honest persistence.** Restores bring back layouts, directories and agents'
  conversations. bmux never claims to revive arbitrary running processes after
  a reboot.
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

## Restore follow-ups

v0.2.0 replaced tmux-resurrect and tmux-continuum: a versioned snapshot
(sessions, split tree, focus, zoom, per-pane cwd), saves every 5 seconds and on
clean shutdown, restore on server start, agent resume (reported commands or
each agent's "continue latest"), and `bmux restart`, which survives protocol
changes and reconnects clients. With an [agent integration](#already-built)
(v0.3.0), two agents in one directory each resume their own session. Since then:

| Item | Size | Notes |
| --- | --- | --- |
| Mark restored sessions | S | Done: `restored` in the picker and sidebar until you attach ([#94](https://github.com/bmux-dev/bmux/issues/94)) |

## Install and updates

**Goal:** install and stay current without Go.
**Done when:** a one-line script installs a verified release binary, and bmux
shows when a newer release is out and updates itself with `bmux update`.

Shipped in v0.5.1:

| Item | Size | Status |
| --- | --- | --- |
| Prebuilt binaries and checksums ([#49](https://github.com/bmux-dev/bmux/issues/49)) | S | Done: GoReleaser builds Linux and macOS on amd64 and arm64 for every `v*` tag, with `checksums.txt` |
| Install script ([#50](https://github.com/bmux-dev/bmux/issues/50)) | S | Done: `curl -fsSL …/install.sh \| sh`, checksum-verified, into `~/.local/bin` without root |
| Update hint and `bmux update` ([#51](https://github.com/bmux-dev/bmux/issues/51)) | M | Done: a small `↑` in the status bar when a release is out (checked once a day, `[update] check`); `bmux update` replaces the binary after checking its checksum, `bmux restart` switches the server |

## v0.4: windows and layouts

**Goal:** organize a project the way tmux users do, with several windows per session.
**Done when:** a session can hold multiple windows, each with its own split
layout, and they survive save/restore.

Shipped in v0.4.0 ([#15](https://github.com/josephferrero/bmux/issues/15)–[#21](https://github.com/josephferrero/bmux/issues/21)):

| Item | Size | Status |
| --- | --- | --- |
| Windows (tabs) per session | L | Done: Ctrl-a c, 1–9, ] / [, Tab, `,` rename, & close; tabs on the top bar; saved and restored |
| Swap and rotate panes | S | Done: Ctrl-a { / }, Ctrl-a Ctrl-o |
| Break and join panes | M | Done: Ctrl-a ! / @ |
| Preset layouts | M | Done: Ctrl-a Space cycles, Alt-1…5 pick, Ctrl-a E equalizes |
| Last pane toggle | S | Done: Ctrl-a ; |
| Project picker | M | Done: the Ctrl-a n popup (Projects and Create tabs) and `bmux project`, from sessions and `projects.roots` |
| Layout files | M | Done: `.bmux.toml`, commands run once trusted; `bmux layout save` / `trust` |

## v0.5: keyboard scrollback

**Goal:** find and copy anything in history without a mouse, and without a mandatory mode.
**Done when:** you can search, select and copy from the keyboard, and the mouse workflow is unchanged.

Shipped in v0.5.0 ([#29](https://github.com/josephferrero/bmux/issues/29)–[#34](https://github.com/josephferrero/bmux/issues/34)):

| Item | Size | Status |
| --- | --- | --- |
| Search history (`/`, `?`, `n`/`N`) | M | Done: Ctrl-a / or / ? in the view, smartcase, wraps around, highlights matches on screen |
| Keyboard selection | M | Done: Ctrl-a v opens the view; vi-style moves, `v`/`V`/Ctrl-v, `y` copies from the server's history |
| Jump between shell prompts | S | Done: `[` / `]` in the view, from OSC 133;A prompt marks |
| Auto-scroll while dragging a selection | S | Done: faster further out; skipped rows copied from the server |
| Reflow on resize | L | Done: wrapped lines rewrap on screen and in scrollback; copies join them |
| Scrollback size setting | S | Done: `scrollback.lines` (100–200,000), applied on `bmux reload` |

## v0.6: scripting

**Goal:** dotfiles and scripts can drive bmux the way they drive tmux.
**Done when:** common tmux snippets (send a command to a pane, capture output,
open a pane in a project folder) have direct bmux equivalents.

Targets depend on windows from v0.4.

Shipped in v0.6.0:

| Item | Size | Status |
| --- | --- | --- |
| Targets for every command | M | Done: `session:window.pane` (`api:2.1`, `api:2.`, `:2.1`); `api:7` stays pane 7 ([#82](https://github.com/bmux-dev/bmux/issues/82)) |
| `send-keys`, `capture-pane` | M | Done: `bmux send` and `bmux capture` take every target form |
| Layout commands | M | Done: `bmux split`, `new-window`, `select`, `kill-pane`, `kill-window`, `resize`, `zoom`, `rename-window`, with targets ([#84](https://github.com/bmux-dev/bmux/issues/84)) |
| `list-panes`, `list-windows`, `display-message` | S | Done: `-F` formats, `-a` for every session, `--json`; `display-message` without `-p` shows in the session's status lines ([#86](https://github.com/bmux-dev/bmux/issues/86)) |
| Format variables | M | Done: `#{session_name}`, `#{window_index}`, `#{pane_current_path}`, `#{pane_current_command}` and 17 more |
| Hooks | M | Done: `[hooks]` in the config file for `session-created`, `session-closed`, `window-created`, `pane-exited`, `client-attached`, `client-detached`, `agent-blocked`, `agent-idle` ([#88](https://github.com/bmux-dev/bmux/issues/88)) |
| `run-shell`, `if-shell` | S | Done: `bmux run-shell` and `bmux if-shell` for a pane, and keys bound to `run-shell COMMAND` ([#90](https://github.com/bmux-dev/bmux/issues/90)) |
| Log a pane to a file | S | Done: `bmux pipe-pane [-o] COMMAND`, e.g. `cat >> pane.log`, and `#{pane_pipe}` ([#92](https://github.com/bmux-dev/bmux/issues/92)) |
| Images in panes | L | Done: the Kitty graphics protocol with Unicode placeholders, stored per pane so images survive scrolling, splits, windows and reattaching; `ui.graphics` ([#47](https://github.com/bmux-dev/bmux/issues/47)) |

## v0.7: alerts and hardening

**Goal:** the last feature release before 1.0, so that 1.0 itself changes nothing.
**Done when:** everything below is in place and nothing in a release breaks
existing sessions without warning.

| Item | Size | Notes |
| --- | --- | --- |
| Activity alerts | M | Like tmux's `monitor-activity`: mark a background pane and its tab when it prints |
| Silence alerts | S | Like `monitor-silence`: mark it when it's been quiet for N seconds, e.g. a build that finished. Agents already get this as "waiting" |
| Compatibility promise | M | Done: save files, configs and commands from every release are tested against today's code (`testdata/releases`, recorded per release); mismatches say to run `bmux restart` ([#112](https://github.com/bmux-dev/bmux/issues/112), [#114](https://github.com/bmux-dev/bmux/issues/114)). See [Compatibility](https://bmux-dev.github.io/bmux/reference/compatibility/) |
| Quality | L | vttest/esctest conformance runs, fuzzing for the protocol and emulator input, memory caps for large scrollback, latency and memory comparison with tmux |
| Linux | S | Daily interactive use on Linux, not only CI. Wayland and X11 clipboard checked |
| Distribution | M | Homebrew tap, signed and notarized macOS binaries, release signatures |
| Docs | M | Demo GIF, tmux migration guide (config and plugin equivalents), CHANGELOG |
| Project | S | CONTRIBUTING.md, issue and PR templates |

## v1.0: stable

**Goal:** safe to recommend to anyone.
**Done when:** v0.7 has been stable and working as expected in daily use. 1.0
adds no features and no changes beyond fixes; it marks the CLI, config and
snapshot formats as stable.

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
| Sixel and iTerm2 images, Kitty direct placements (images today use Kitty's Unicode placeholders) | `allow-passthrough` |

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
| `new-window`, `select-window`, `last-window`, `rename-window`, `kill-window`, window list | Ctrl-a c, 1–9, ] / [, Tab, `R`, &; tabs on the top bar with a `!` for windows waiting on you |
| `swap-pane`, `rotate-window`, `break-pane`, `join-pane`, `last-pane`, `select-layout` (incl. `-E`) | Ctrl-a { / }, Ctrl-a Ctrl-o, Ctrl-a ! / @, Ctrl-a ;, Ctrl-a Space / Alt-1…5 / E |
| sesh, tmux-sessionizer | Project picker: Ctrl-a n, `bmux project [DIR]` |
| tmuxinator, tmuxp | `.bmux.toml` layout files with trusted commands; `bmux layout save` |
| `focus-events on`, vim-tmux-focus-events | Focus in/out (`?1004`) for the focused pane while a client's terminal has focus |
| `set-titles on` | Window title from the session and the focused pane's title; `ui.set-title` turns it off |
| vim-tmux-navigator | Vim/Neovim-aware Ctrl-h/j/k/l, edge-crossing via [an editor mapping](https://bmux-dev.github.io/bmux/guides/editor-navigation/), `keys.pass-through` for other programs (its `is_vim` pattern) |
| tmux-yank, `set-clipboard` | Copy on mouse release: pbcopy, wl-copy, xclip, xsel or OSC 52; apps' OSC 52 writes (Neovim yanks) copied by every attached client; copies join soft-wrapped lines |
| tmux-better-mouse-mode, `mouse on` | Click to focus, drag dividers to resize, drag/double/triple-click to select, drag past the edge to auto-scroll, wheel to scroll; Alt overrides mouse-aware apps |
| `copy-mode -e` with vi keys, tmux-copycat, `search-backward` | Optional scrollback view (Ctrl-a v): vi moves, `v` / `V` / Ctrl-v, `y` copies; search with Ctrl-a / or `/` `?` `n` `N`; `[` / `]` jump between shell prompts (OSC 133) |
| `history-limit`, rewrap on resize | `scrollback.lines`; wrapped lines reflow on screen and in scrollback |
| tmux-notify (agents) | Sound when a background agent becomes ready or blocked |
| tmux-prefix-highlight | Status line shows prefix keys while Ctrl-a is pending |
| `.tmux.conf`, `source-file` | `~/.config/bmux/config.toml` (Helix layout), reload with Ctrl-a r, `bmux reload` or SIGUSR1; `bmux default-config` |
| `bind-key`, `unbind-key`, `list-keys` (`C-b ?`) | `[keys.root]` and `[keys.prefix]` in the config file; Ctrl-a ? lists the bindings in effect |
| `send-keys`, `capture-pane`, `split-window`, `new-window`, `list-panes`, `display-message -p`, formats, `set-hook`, `run-shell`, `if-shell`, `pipe-pane` | `bmux send`, `capture`, `split`, `new-window`, `list-panes`, `display-message`, `#{...}` variables, `[hooks]`, `run-shell`, `if-shell`, `pipe-pane`, with `session:window.pane` targets |
| Status themes | Powerline status bars, accent colors, light theme |
| (no tmux equivalent) | Images programs draw in panes (Kitty graphics), kept per pane through scrolling, splits and detaching |

v0.1.1 fixed palette colors ignoring the terminal theme, `bmux focus` after a
rename, directional navigation while zoomed, and `bmux --help` wording.

v0.2.0 added the agent sidebar, restore across restarts and reboots with
`bmux restart`, flicker-free synchronized output, and app backgrounds set with
OSC 11.

v0.3.0 added the config file with rebindable keys and a key list (Ctrl-a ?),
agent integrations for Claude Code, Codex, omp, opencode and pi with an
integrations panel (Ctrl-a i) and a welcome screen, agents supervising other
agents (`bmux agents`, `capture`, `send`, `wait-agent`), splits in the focused
pane's directory, dragging dividers, Ctrl-a x, Ctrl-a ( / ), refusing to nest,
Ctrl-h/j/k/l pass-through, focus events, window titles, apps' OSC 52 copies and
alert sounds on Linux.

v0.4.0 added windows (tabs) per session, swapping, rotating, breaking and
joining panes, preset layouts, the last-pane toggle, the project picker
(Ctrl-a n, `bmux project`), `.bmux.toml` layout files, deleting sessions from
the sidebar, and window counts in `bmux list`.

v0.5.0 added the scrollback view (Ctrl-a v) with vi-style keyboard selection,
history search (Ctrl-a /), jumps between shell prompts, auto-scroll while
dragging a selection, reflow on resize with copies that join wrapped lines,
and the `scrollback.lines` setting.

v0.5.1 added prebuilt Linux and macOS binaries with checksums, a one-line
install script, a once-a-day check for new releases with a hint in the status
bar, and `bmux update`.

v0.5.2 added the settings page (Ctrl-a ,), one new-session popup for finding
a project or creating a session (Ctrl-a n) with `projects.start-dir`,
clickable links, modified keys for programs that ask for them (kitty keyboard
protocol, `modifyOtherKeys`), `bmux zoomed`, and `agent:blocked` for omp and
pi extensions. Protocol version 8: run `bmux restart` after upgrading.

v0.6.0 added `session:window.pane` targets for every scripting command, layout
commands (`bmux split`, `new-window`, `select`, `kill-pane`, `kill-window`,
`resize`, `zoom`, `rename-window`), `#{...}` format variables with
`list-panes`, `list-windows` and `display-message`, `[hooks]` for session,
window, pane, client and agent events, `run-shell`, `if-shell` and keys bound to
shell commands, `pipe-pane`, `restored` marks on sessions in the picker and
sidebar, and images in panes with the Kitty graphics protocol (`ui.graphics`).
Protocol version 10: run `bmux restart` after upgrading.
