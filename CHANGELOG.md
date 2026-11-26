# Changelog

All releases during `0.x` are pre-releases on [GitHub Releases](https://github.com/bmux-dev/bmux/releases). When the protocol version changes between releases, run `bmux restart` after upgrading to switch the server onto the new version.

## v0.7.7 — 2026-11-26

- Added an opt-in beta release channel (`bmux update --beta`, `--stable`, `[update] channel`, `BMUX_UPDATE_CHANNEL`, and `BMUX_CHANNEL=beta` in `install.sh`) plus a manual `.github/workflows/beta.yml` workflow that builds and publishes pre-releases without touching Homebrew.
- Added a hard per-pane scrollback memory cap (`scrollback.max-mb`, default `64` MiB, `0` = unlimited, live-reloadable via `bmux reload`).
- Hardened untrusted input and CI boundaries: context-aware shell quoting for `#{...}` plus `BMUX_PANE_*` environment variables in hooks/`run-shell`/`if-shell`/`pipe-pane`, CSI repeat-count caps, bracketed-paste end-marker stripping, C1 control filtering in the status bar, 4 KiB OSC field caps, Kitty file-path rules & raw image dimension overflow checks, `layout save` trust preservation and bounded non-blocking layout reads, atomic `F_DUPFD_CLOEXEC` PTY duplication, state-directory permission checks, pinned GitHub Actions SHAs with Dependabot, and `Sync()` before swapping updated binaries.
- Fixed Ctrl-click link copying without a browser (OSC 52) and Homebrew Caskroom detection in `bmux update`; overhauled `README.md`, live-agent workflow demo, `CONTRIBUTING.md`, and `CHANGELOG.md`.
- Protocol version 11 unchanged: run `bmux restart` when convenient to bring the server onto `v0.7.7`.

## v0.7.6 — 2026-11-25

- Added first-class Google Antigravity CLI (`agy`) support: foreground detection, `agy --continue` restore, lifecycle hooks in `~/.gemini/config/hooks.json`, and the `bmux` skill via `bmux integration install agy`.
- Migrated the Go module path to `github.com/bmux-dev/bmux` and added the Homebrew cask tap (`brew install --cask bmux-dev/tap/bmux`).
- Launched the redesigned documentation site with 2× Retina screenshots and workflow demo, and fixed pasting into the focused field of the new-session Create tab.
- Protocol version 11 unchanged: run `bmux restart` when convenient to bring the server onto `v0.7.6` for `agy` detection and resume.

## v0.7.5 — 2026-11-24

- Overhauled VT cells, scrollback storage, event-driven frame push, row-delta protocol updates, and client differential rendering: `seq 1 200000` runs in `0.06 s` (down from `0.40 s`), slowest keystroke echoes drop from `37 ms` to `~5 ms`, 10k lines of colored scrollback use `26 MiB` (down from `115 MiB`), and the server stays idle at `0 Hz` when nothing changes.
- Protocol version 11 replaces 10: run `bmux restart` after upgrading.

## v0.7.4 — 2026-11-09

- Added opt-in client and server CPU, allocation, and heap profiles through private Unix sockets (`BMUX_PPROF_DIR`), plus `scripts/profile-bmux.py` and isolated profiling instructions. Profiling is disabled by default.
- Protocol version 10 unchanged.

## v0.7.3 — 2026-11-09

- Fixed OpenCode V2 plugin loading and added explicitly bound pane-state reporting, safe managed-plugin updates, and V1 compatibility. Update with `bmux integration install opencode`; exact V2 reporting requires a bound standalone conversation.
- Protocol version 10 unchanged.

## v0.7.2 — 2026-11-03

- Grouped sessions in git worktrees under their main repository in the sidebar, session picker, and Projects tab, with branch labels (`ui.show-branch`) and single-click sidebar opening.
- Improved scrolling output speed by 6.6× and shipped 9 terminal conformance fixes found by `esctest` (`reset`, insert mode, NEL, ED 1/3, `CSI u`, origin mode, and mode 47 alternate screen).
- Protocol version 10 unchanged: run `bmux restart` after upgrading for the new server behavior.

## v0.7.1 — 2026-10-26

- Improved upgrade diagnostics when running an older server after `bmux update` (advising `bmux restart` instead of a bare protocol mismatch error) and stopped `bmux restart` from reporting lost changes already saved by the old server.
- Changed sidebar mouse behavior to select on single click and open on double click (later refined to single-click open in `v0.7.2`).
- Added release compatibility tests checking prior save files, configs, and commands (`testdata/releases/`).
- Protocol version 10 unchanged.

## v0.7.0 — 2026-10-22

- Added 25 theme presets across common Neovim colorschemes (`Catppuccin`, `Tokyo Night`, `Gruvbox`, `Nord`, `Rosé Pine`, `Kanagawa`, `One Dark`, `Dracula`, `Everforest`, `Solarized`) plus `bmux theme NAME` for script/Neovim `ColorScheme` switching.
- Added pane coloring in theme colors (`ui.theme-panes`, `ui.pane-background`) with honest OSC 10/11 replies, bordered floating notice boxes (`ui.notice-position`), and a redesigned settings page with tabs, icons (`ui.icons`), group headers, and live previews.
- Protocol version 10 unchanged: run `bmux restart` after upgrading for the new server behavior.

## v0.6.0 — 2026-10-22

- Added `session:window.pane` targets across scripting commands (with `bmux send` requiring explicit pane targets), layout commands (`bmux split`, `new-window`, `select`, `kill-pane`, `kill-window`, `resize`, `zoom`, `rename-window`), `#{...}` format variables with `list-panes`, `list-windows`, and `display-message` (now displayed in the notice box when `-p` is omitted).
- Added `[hooks]` for session, window, pane, client, and agent events, `run-shell`/`if-shell` (including keybindings), `pipe-pane`, `restored` session badges, and Kitty graphics protocol image retention per pane (`ui.graphics`).
- Protocol version 10: run `bmux restart` after upgrading.

## v0.5.2 — 2026-10-21

- Added the settings UI (`Ctrl-a ,`), the unified new-session/project popup (`Ctrl-a n`) with `projects.start-dir`, clickable terminal links, extended keyboard protocols (kitty keyboard protocol, `modifyOtherKeys`), `bmux zoomed`, and `agent:blocked` state reporting for `omp` and `pi` extensions.
- Protocol version 8: run `bmux restart` after upgrading.

## v0.5.1 — 2026-10-20

- Added GoReleaser prebuilt Linux/macOS binaries with SHA-256 `checksums.txt`, the checksum-verified `install.sh` one-liner, daily release checks (`update.check`), and `bmux update`.

## v0.5.0 — 2026-10-20

- Added the keyboard scrollback view (`Ctrl-a v`) with vi navigation and `v`/`V`/`Ctrl-v` visual selection, history search (`Ctrl-a /`), OSC 133 prompt jumps (`[`/`]`), drag-selection auto-scroll, resize reflow with wrapped-line join on copy, and `scrollback.lines`.

## v0.4.0 — 2026-10-20

- Added multiple windows (tabs) per session, pane swapping/rotating/breaking/joining, preset layouts (`Ctrl-a Space`, `Ctrl-a Alt-1…5`, `Ctrl-a E`), last-pane toggle (`Ctrl-a ;`), the project picker (`Ctrl-a n`, `bmux project`), `.bmux.toml` trusted layout files (`bmux layout save` / `trust`), deleting sessions from the sidebar, and window counts in `bmux list`.

## v0.3.0 — 2026-10-19

- Added `~/.config/bmux/config.toml` with rebindable keys and `Ctrl-a ?`, agent integrations (`Ctrl-a i`) for Claude Code, Codex, `omp`, OpenCode, and `pi` with a welcome screen, agent supervision commands (`bmux agents`, `capture`, `send`, `wait-agent`), focused-directory splits, divider dragging, `Ctrl-a x`, `Ctrl-a (` / `)`, nesting prevention, `Ctrl-h/j/k/l` Vim/Neovim pass-through, focus events, window titles, app OSC 52 copies, and Linux alert sounds.

## v0.2.0 — 2026-10-19

- Added the agent sidebar, snapshot save/restore across restarts and reboots (`bmux restart`), synchronized output rendering, and OSC 11 app backgrounds.

## v0.1.1 — 2026-10-19

- Fixed palette colors ignoring the terminal theme, `bmux focus` after session rename, directional navigation while zoomed, and `bmux --help` wording.

## v0.1.0 — 2026-10-17

- Initial release with persistent client-server sessions, live-preview session picker, mouse selection without copy mode, and automatic save/restore.
