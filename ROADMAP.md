# Roadmap

bmux is the batteries-included terminal multiplexer built for coding agents —
persistent sessions, git worktree grouping, a live-preview session picker, an
agent status sidebar, and modeless mouse selection, with no plugin manager.

## Now, next, later

| | Version | Focus |
| --- | --- | --- |
| **Shipped** | [v0.7.6](#already-built) | Antigravity (`agy`) support, Homebrew tap, `bmux-dev/bmux` module path, redesigned docs & demo ([Releases](https://github.com/bmux-dev/bmux/releases)) |
| **Now** | [v0.8](#v08-hardening) | Everything 1.0 needs: memory caps, bug fixes, compatibility, quality |
| **Later** | [v1.0](#v10-stable) | v0.8, once it's stable: no new features |
| | [Ideas](#ideas) | Worktree actions, popups, hints, multi-client, notifications |

## v0.8: hardening

**Goal:** everything 1.0 needs, so that 1.0 itself changes nothing.
**Done when:** every item below is complete and no release breaks existing
sessions without warning.

Sizes are rough: **S** is a day or less, **M** a few days, **L** a week or more.

| Item | Size | Status | Notes |
| --- | --- | --- | --- |
| Scrollback memory caps | M | Done | Configurable `scrollback.max-mb` per-pane byte cap (default 64 MiB, `0` = unlimited) in `internal/vt` and `config.toml` ([#157](https://github.com/bmux-dev/bmux/issues/157)), building on v0.7.5's 4.5× cell-memory reduction |
| Open bugs & CI stability | S | Done | Ctrl-click over SSH or headless sessions copies unwrapped links via OSC 52 ([#183](https://github.com/bmux-dev/bmux/issues/183)); fixed `TestAttachedClientFocusControlsViewing` FIFO barrier ([#150](https://github.com/bmux-dev/bmux/issues/150)) |
| Linux interactive QA | S | In progress | End-to-end Wayland (`wl-copy`) and X11 (`xclip`/`xsel`) clipboard and multi-terminal SSH verification |
| Project hygiene | S | Done | Root [CONTRIBUTING.md](https://github.com/bmux-dev/bmux/blob/main/CONTRIBUTING.md) and `.github/` issue/PR templates (full guide on the [docs site](https://bmux-dev.github.io/bmux/project/contributing/)) |
| Compatibility promise | M | Done | Save files, configs, and commands tested per release in `testdata/releases` ([#112](https://github.com/bmux-dev/bmux/issues/112), [#114](https://github.com/bmux-dev/bmux/issues/114); `v0.7.3` omitted as identical to `v0.7.2`). See [Compatibility](https://bmux-dev.github.io/bmux/reference/compatibility/) |
| Quality & performance | L | Done | Fuzzing for session requests, parsers, and emulator input ([#133](https://github.com/bmux-dev/bmux/issues/133)), `esctest` conformance (`scripts/esctest.sh`), tmux benchmark suite (`scripts/compare-tmux.py`), and v0.7.5 performance overhaul ([#190](https://github.com/bmux-dev/bmux/pull/190)) |
| Docs & changelog | M | Done | Redesigned docs site, live-agent demo video, [tmux migration guide](https://bmux-dev.github.io/bmux/start/coming-from-tmux/), and [CHANGELOG.md](https://github.com/bmux-dev/bmux/blob/main/CHANGELOG.md) |

## v1.0: stable

**Goal:** safe to recommend to anyone.
**Done when:** v0.8 has been stable and working reliably in daily use. 1.0 adds
no features and no changes beyond bug fixes; it marks the CLI, config, and
snapshot formats as stable.

## Ideas

Worth doing, not yet scheduled. Open or upvote an issue if one matters to you.

| Area | Idea | Notes / replaces |
| --- | --- | --- |
| Agents & worktrees | Create and clean up git worktrees from `Ctrl-a n` | Spawn a branch worktree + agent session in one flow, and remove merged worktree sessions |
| Agents & worktrees | Single-key triage to jump to the next `blocked` or `waiting` agent | Cycle across sessions needing input with one shortcut |
| Agents & worktrees | Selective agent integration installer during onboarding | Choose which integrations to install in `Ctrl-a i` ([#41](https://github.com/bmux-dev/bmux/issues/41)) |
| Agents & worktrees | Additional managed agent integrations (e.g. Gemini CLI) + open hook contract | Process detection already works; add hook reporting and exact-session resume |
| Agents & worktrees | Forward OSC 9/777 desktop notifications to the outer terminal | Today OSC 9/777 marks the sidebar/tab, agents chime, and `[hooks]` can run `notify-send` (`tmux-notify`) |
| Workspace & layouts | Floating popups for `lazygit`, `fzf`, and scratch shells | `display-popup`, `tmux-floax` |
| Workspace & layouts | Command menus | `display-menu` |
| Workspace & layouts | Keyboard hint mode for URLs, paths, hashes, and IPs | `tmux-thumbs`, `tmux-fingers`, `tmux-open`, `extrakto` |
| Workspace & layouts | Configurable `[default-layout]` and fixed `rows`/`cols` splits | Session defaults in `config.toml` ([#42](https://github.com/bmux-dev/bmux/issues/42)) |
| Terminal & sessions | User-configurable status bar segments (git branch, battery, shell/format segments) | `tmux-power`, `status-left`/`status-right` (host, clock, and prefix hint are already built in) |
| Terminal & sessions | Per-client window size and focus | `window-size`, `aggressive-resize` |
| Terminal & sessions | Read-only attach and detach other clients | `attach -r`, `detach -a` |
| Terminal & sessions | Type into every pane at once | `synchronize-panes` |
| Terminal & sessions | Lock a session | `lock-session` |
| Terminal & sessions | Nested SSH key-interception toggle | Pass prefix/root keys straight to an outer/inner bmux session over SSH |
| Terminal & sessions | Sixel, iTerm2 images, and Kitty direct placements | Images today use Kitty Unicode placeholders (`allow-passthrough`) |
| Packaging | Signed & notarized macOS binaries and release signatures | Apple Developer ID notarization + `cosign` signatures (SHA-256 checksums and `brew install --cask bmux-dev/tap/bmux` are shipped) |
| Packaging | Native Linux distro packages | AUR, Nix flake, `.deb`/`.rpm` via GoReleaser nFPM |

## Principles

- **Batteries included.** If tmux needs a plugin for it, bmux builds it in.
- **Modeless by default.** Keyboard selection and search can exist, but copy mode
  is never required.
- **Server owns terminals, client owns the clipboard.**
- **Honest persistence.** Restores bring back layouts, directories, and agents'
  conversations. bmux never claims to revive arbitrary running processes after
  a reboot.
- **Opinionated defaults, then configuration.** Defaults should match a good
  tmux setup so most people never need a config file.

## Not planned

| | Why |
| --- | --- |
| Native Windows | macOS and Linux only. WSL may work, though it isn't tested. |
| Plugin manager | The goal is to make plugins unnecessary. Scripting and hooks cover custom glue; see [scripting](https://bmux-dev.github.io/bmux/agents/scripting/). |
| tmux config compatibility | bmux won't parse `.tmux.conf`. A [migration guide](https://bmux-dev.github.io/bmux/start/coming-from-tmux/) maps common settings instead. |
| Remote or browser sharing | Use SSH plus `bmux attach`. |
| Reviving processes after reboot | Not possible in general. See [principles](#principles). |

## Already built

| Milestone | Highlights |
| --- | --- |
| **v0.7.6** | First-class Antigravity (`agy`) detection, hooks, skill, and `agy --continue` restore; Homebrew cask tap (`brew install --cask bmux-dev/tap/bmux`); `github.com/bmux-dev/bmux` module migration; redesigned docs site and demo |
| **v0.7.5** | 6.9× faster build output (`200k` lines in `0.06 s`), `~1.5 ms` median echo latency, `~4.5×` lower scrollback RAM (`26 MiB` vs `115 MiB`), `0 Hz` idle redraws, and Protocol v11 row deltas ([#190](https://github.com/bmux-dev/bmux/pull/190)) |
| **v0.7.0–v0.7.4** | Git worktree grouping & single-click sidebar (`v0.7.2`, [#149](https://github.com/bmux-dev/bmux/issues/149)), 25 Neovim colorscheme presets & `bmux theme`, floating notice boxes (`ui.notice-position`), OpenCode V2 integration, `esctest` conformance, compatibility fixture tests, and `BMUX_PPROF_DIR` profiling |
| **v0.6** | `session:window.pane` targets (explicit pane targets required for `bmux send`), layout commands (`split`, `new-window`, `resize`, `zoom`), 22 `#{...}` formats, `[hooks]`, `run-shell`/`if-shell`, `pipe-pane`, `restored` badges, and per-pane Kitty graphics retention ([#82](https://github.com/bmux-dev/bmux/issues/82)–[#92](https://github.com/bmux-dev/bmux/issues/92), [#47](https://github.com/bmux-dev/bmux/issues/47)) |
| **v0.5** | Keyboard scrollback view (`Ctrl-a v`) with vi selection and `/` search, OSC 133 `[`/`]` prompt jumps, resize reflow, prebuilt GoReleaser binaries, `install.sh`, `bmux update`, settings page (`Ctrl-a ,`), `Ctrl-a n` new-session/project popup, and clickable links ([#29](https://github.com/bmux-dev/bmux/issues/29)–[#34](https://github.com/bmux-dev/bmux/issues/34), [#49](https://github.com/bmux-dev/bmux/issues/49)–[#51](https://github.com/bmux-dev/bmux/issues/51)) |
| **v0.4** | Windows (tabs), swap/rotate/break/join panes, preset layouts (`Ctrl-a Space`, `Ctrl-a Alt-1…5`, `Ctrl-a E`), project picker (`bmux project [DIR]`), and trusted `.bmux.toml` layouts ([#15](https://github.com/bmux-dev/bmux/issues/15)–[#21](https://github.com/bmux-dev/bmux/issues/21)) |
| **v0.3** | `~/.config/bmux/config.toml`, `Ctrl-a ?`, agent integrations (`Ctrl-a i`), supervision CLI (`bmux agents`, `capture`, `send`, `wait-agent`), Vim/Neovim-aware `Ctrl-h/j/k/l`, `?1004` focus events, and OSC 52 clipboard forwarding |
| **v0.2 & v0.1** | Persistent client-server sessions, list-first session picker with live preview, modeless mouse selection (`Alt`/`Shift` overrides mouse-aware apps), agent status sidebar, and snapshot save/restore (`bmux restart`) |

See [Coming from tmux](https://bmux-dev.github.io/bmux/start/coming-from-tmux/) for the plugin-by-plugin comparison, and [CHANGELOG.md](https://github.com/bmux-dev/bmux/blob/main/CHANGELOG.md) or [GitHub Releases](https://github.com/bmux-dev/bmux/releases) for full release notes.
