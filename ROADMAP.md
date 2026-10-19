# Roadmap

bmux aims to cover what people use tmux **plus its most popular plugins** for,
with everything built in and no plugin manager.

## Now, next, later

| | Version | Theme |
| --- | --- | --- |
| **Now** | [v0.1.x](#v01x-fixes) | Fix known bugs in the first release |
| **Next** | [v0.2](#v02-daily-driver) | Daily driver: replace tmux for a full working day |
| | [v0.3](#v03-survive-reboots-and-upgrades) | Survive reboots and upgrades (resurrect + continuum) |
| | [v0.4](#v04-windows-and-layouts) | Windows and layouts |
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

## v0.1.x: fixes

**Goal:** the published release does what its docs say.
**Done when:** every item below is fixed with a regression test and released as v0.1.1.

| Item | Size | Notes |
| --- | --- | --- |
| Renaming a session breaks `bmux focus` | S | Panes keep the old name in `BMUX_SESSION`. Resolve by a stable session ID instead of the name. |
| Directional pane navigation does nothing while zoomed | S | Only the zoomed pane is laid out. Match tmux: unzoom, then move. |
| `bmux --help` wording | S | Ctrl-a p is "last session", and `kill` stops every pane, not just "its shell". |
| Document that Ctrl-l doesn't clear the shell | S | Already in the docs. The real fix is rebinding in v0.2. |

## v0.2: daily driver

**Goal:** a tmux user can switch to bmux for a full day without missing anything basic.
**Done when:** the owner uses bmux instead of tmux for a week of real work.

### Panes and sessions

| Item | Size | Notes |
| --- | --- | --- |
| Close pane (`Ctrl-a x`) | S | Confirm when something other than the shell is running. |
| New panes open in the focused pane's directory | M | Like tmux's `-c "#{pane_current_path}"`. Needs per-pane cwd on macOS and Linux. v0.3 depends on this. |
| Drag dividers with the mouse to resize | M | Most `mouse on` tmux users expect this. |
| Next/previous session (`Ctrl-a )` / `(`) | S | Same keys as tmux. Ctrl-a p stays "last session". |
| Refuse to nest | S | Running `bmux` inside a bmux pane warns instead of starting a client in a pane, like tmux checks `$TMUX`. |

### Keys and config

| Item | Size | Notes |
| --- | --- | --- |
| Config file `~/.config/bmux/config.toml` | M | Replaces the `BMUX_*` variables. The variables keep working as overrides. |
| Choose the prefix key | S | Ctrl-a by default; tmux's Ctrl-b is a common request. |
| Rebind keys, including turning off Ctrl-h/j/k/l | M | Fixes the Ctrl-l conflict. Rebinding everything can wait. |
| Pass-through list for Ctrl-h/j/k/l | S | Forward to fzf and other listed programs, like vim-tmux-navigator's `is_vim`. |
| Reload config (`Ctrl-a r`, `bmux reload`) | S | Like tmux's `source-file`. |
| Key help overlay (`Ctrl-a ?`) | S | Lists current bindings. |

### Terminal behavior apps rely on

| Item | Size | Notes |
| --- | --- | --- |
| Focus events (`?1004`) | S | Neovim's `autoread` and `FocusGained` need them. |
| Clipboard writes from apps (OSC 52) | M | e.g. Neovim yanking over SSH. Routed through the attached client. |
| Set the outer terminal's title | S | Like tmux's `set-titles`. |

## v0.3: survive reboots and upgrades

**Goal:** a reboot or a bmux upgrade costs you seconds, not your layout.
**Done when:** after rebooting, `bmux` brings back every session with its splits,
directories and allowlisted programs.

Replaces tmux-resurrect and tmux-continuum. Depends on per-pane cwd from v0.2.

| Item | Size | Notes |
| --- | --- | --- |
| Snapshot format | M | Sessions, split tree, focused pane, per-pane cwd, window list once v0.4 lands. Versioned so newer releases can read old snapshots. |
| `bmux save` / `bmux restore` | M | Restored panes start a fresh shell in the saved directory. |
| Relaunch allowlist | M | e.g. `nvim -S`, `less`, `man`, `ssh host`. Everything else restores as a shell. |
| Optional scrollback in snapshots | M | Like resurrect's `capture-pane-contents`. Off by default; has a size cap. |
| Automatic saves | S | Periodic (15 minutes by default) and on clean server shutdown. |
| Restore on server start | S | On by default. The picker marks restored sessions. |
| Safe upgrades | M | Today a protocol change means stopping the server and losing sessions. Save before stopping, restore after, and say so in the error message. |

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
| Layout files | M | Declare a project's windows, panes and commands once. Replaces tmuxinator and tmuxp. Shares the v0.3 snapshot format. |

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
| `send-keys`, `capture-pane` | M | |
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
| Compatibility promise | Stable CLI, config format and snapshot format. Protocol changes never silently cost sessions (builds on v0.3 safe upgrades). |
| Terminal support | Extended keys (CSI u / kitty keyboard), hyperlinks (OSC 8), synchronized output (`?2026`), bell and activity alerts, opt-in graphics passthrough. |
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
| Desktop notifications when a job or AI agent finishes (OSC 9/777) | tmux-notify |
| Theme presets and custom status segments (host, time, prefix indicator) | tmux-power, tmux-prefix-highlight |
| Per-client window size and focus | `window-size`, `aggressive-resize` |
| Read-only attach, detach other clients | `attach -r`, `detach -a` |
| Type into every pane at once | `synchronize-panes` |
| Lock a session | `lock-session` |

## Already built

| tmux / plugin | bmux |
| --- | --- |
| Sessions, detach/attach, `new`/`list`/`attach`/`rename`/`kill` | Built in |
| tmux-sessionx / tmux-fzf session picker | Session picker with fuzzy filter and live preview |
| `split-window`, `select-pane`, `resize-pane` (repeatable), zoom | `Ctrl-a \|` / `-`, `Ctrl-a` arrows / `o`, `Ctrl-a h/j/k/l`, `Ctrl-a m` |
| vim-tmux-navigator | Vim/Neovim-aware Ctrl-h/j/k/l, edge-crossing via [an editor mapping](docs/editor-navigation.md) |
| tmux-yank | Copy on mouse release: pbcopy, wl-copy, xclip, xsel or OSC 52 |
| Mouse mode | Click to focus, drag to select, wheel to scroll; Alt overrides mouse-aware apps |
| Status themes | Powerline status bars, accent colors, light theme |
