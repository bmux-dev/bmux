# Roadmap

bmux aims to cover what people use tmux **plus its most popular plugins** for,
with everything built in. This file tracks the gap to that goal.

Guiding rules (see also [AGENTS.md](AGENTS.md)):

- Batteries included: a feature that's a plugin in tmux should be built in here.
- Modeless by default. Keyboard selection and search may exist, but copy mode is
  never mandatory.
- Terminal state lives on the server; clipboard access lives in the attached client.
- Saved workspaces restore layouts, directories and allowlisted commands. They
  never claim to restore arbitrary running processes after a reboot.

## Already built

| tmux / plugin | bmux |
| --- | --- |
| Sessions, detach/attach, `new`/`list`/`attach`/`rename`/`kill` | Built in |
| tmux-sessionx / tmux-fzf session picker | Session picker with fuzzy filter and live native preview |
| `split-window`, `select-pane`, `resize-pane` (repeatable), zoom | `Ctrl-a \|` / `-`, `Ctrl-a` arrows / `o`, `Ctrl-a h/j/k/l`, `Ctrl-a m` |
| vim-tmux-navigator | `Ctrl-h/j/k/l` with Vim/Neovim foreground detection and `bmux focus` |
| tmux-yank | Copy on mouse release; pbcopy, wl-copy, xclip, xsel or OSC 52 |
| Mouse mode | Click to focus, drag to select, wheel to scroll; Alt overrides mouse-aware apps |
| tmux-power / status themes | Powerline top bar and status row, accents, light theme |

## Milestone 1: daily-driver basics

Small changes that fix friction you hit every day.

- [ ] **Close pane** key (`kill-pane`, `Ctrl-a x`) with confirmation when a job is running.
- [ ] **New panes start in the focused pane's working directory** (tmux's
      `-c "#{pane_current_path}"`). Needs per-pane cwd tracking on macOS and
      Linux, which Milestone 2 also depends on.
- [ ] **Focus events** (`focus-events on`): forward FocusIn/FocusOut (`?1004`) to
      apps that request them, so Neovim `autoread`/`FocusGained` work.
- [ ] **Application OSC 52**: forward clipboard writes from apps (e.g. Neovim yank
      over SSH) through the attached client.
- [ ] Next/previous session keys (`switch-client -n/-p`).
- [ ] Forward `fzf` (and other configurable programs) like editors for
      `Ctrl-h/j/k/l`, matching vim-tmux-navigator's default `is_vim` list.

## Milestone 2: workspaces that survive reboot (tmux-resurrect + tmux-continuum)

- [ ] Workspace snapshot format: sessions, split layout, focused pane, per-pane cwd.
- [ ] Optional scrollback capture per pane (resurrect's `capture-pane-contents`).
- [ ] Allowlist of commands to relaunch on restore (e.g. `nvim -S`, `less`,
      `man`, `ssh host`); everything else restores as a fresh shell in the same
      directory.
- [ ] Automatic periodic snapshots (continuum's 15-minute save) plus a save on
      clean shutdown.
- [ ] Restore on server start (continuum's `continuum-restore on`), with a
      `bmux restore` command and a picker indicator for restored sessions.
- [ ] `bmux save` / `bmux restore` CLI.

## Milestone 3: windows and layouts

- [ ] **Windows/tabs** per session: new, next/previous, select by number,
      rename, close, and a window list in the status bar.
- [ ] `swap-pane`, `rotate-window`.
- [ ] `break-pane` (pane → new window) and `join-pane`.
- [ ] Preset layouts: even-horizontal, even-vertical, main-horizontal,
      main-vertical, tiled; a key to cycle them and one to equalize.
- [ ] Last pane / last window toggles.

## Milestone 4: keyboard scrollback and search

Optional, never a required copy mode.

- [ ] Search scrollback (`/`, `?`, `n`/`N`) with highlighted matches.
- [ ] Keyboard selection (vi-style `v`/`y`, line and block selection).
- [ ] Jump between shell prompts (OSC 133 marks).
- [ ] Selection auto-scroll while dragging.
- [ ] Scrollback reflow on resize; soft-wrapped lines copy without newlines.

## Milestone 5: configuration

- [ ] Config file (e.g. `~/.config/bmux/config.toml`) replacing `BMUX_*` env vars.
- [ ] Rebindable keys, including prefix choice, with a reload key/command
      (tmux `source-file`).
- [ ] Options: scrollback size, status position, theme/accent, mouse behavior,
      default shell, default directory.
- [ ] Theme presets (tmux-power style: violet, gold, redwine, etc.) and
      configurable status segments: session, window list, host, user, time,
      prefix indicator (tmux-prefix-highlight).

## Milestone 6: scripting and automation

The tmux command surface that most plugins and dotfiles depend on.

- [ ] Targets (`session:window.pane`) for every CLI command.
- [ ] `send-keys`, `capture-pane`, `list-panes`, `list-windows`, `display-message`.
- [ ] Format variables (`#{session_name}`, `#{pane_current_path}`, `#{pane_id}`, …).
- [ ] `run-shell`, `if-shell`.
- [ ] Hooks (`after-new-session`, `pane-exited`, `client-attached`, …).
- [ ] `pipe-pane` and "save pane history to a file" (tmux-logging).

## Milestone 7: popups and hints

- [ ] Floating popups (`display-popup`) for lazygit, fzf and scratch shells (tmux-floax).
- [ ] Menus (`display-menu`) and a key-binding help overlay.
- [ ] Hint mode: label URLs, paths, git hashes and IPs on screen, then copy or
      open them with one key (tmux-thumbs / tmux-fingers / tmux-open / extrakto).

## Milestone 8: terminal compatibility

- [ ] Extended keys: CSI u / kitty keyboard protocol passthrough.
- [ ] Set the outer terminal's title (`set-titles`).
- [ ] Bell and activity/silence monitoring with status indicators.
- [ ] Hyperlinks (OSC 8).
- [ ] Synchronized output passthrough (`?2026`).
- [ ] Opt-in passthrough for graphics protocols (`allow-passthrough`).
- [ ] Linux exercised in day-to-day interactive use (CI already runs on Ubuntu).

## Milestone 9: multiple clients

- [ ] Per-client sizing policy (`window-size latest|smallest|largest`,
      `aggressive-resize`).
- [ ] Per-client pane focus option.
- [ ] Read-only attach and "detach other clients".
- [ ] `synchronize-panes` (type into every pane of a window).
- [ ] Lock session/client.

## Release and distribution

- [ ] Tagged releases with prebuilt macOS/Linux binaries (GoReleaser).
- [ ] Homebrew tap (`brew install josephferrero/tap/bmux`).
- [x] [Neovim navigation snippet documented](docs/editor-navigation.md) for `Ctrl-h/j/k/l` edge crossing.
- [ ] Migration guide: tmux config and plugin → bmux equivalent.
