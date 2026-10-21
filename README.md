# bmux

**The batteries-included terminal multiplexer.**

[![CI](https://github.com/josephferrero/bmux/actions/workflows/ci.yml/badge.svg)](https://github.com/josephferrero/bmux/actions/workflows/ci.yml)

bmux brings common tmux-plugin workflows into one program. Browse sessions with
live previews, watch your coding agents from a sidebar, copy text with the
mouse, and keep your layout across restarts, without a plugin manager.

**v0.5.1 pre-release for macOS and Linux.**

**[Documentation](https://bmux-dev.github.io/bmux/)**

<!-- TODO: demo GIF: picker with live preview → attach → split → drag to copy -->

## Built in

Everything below would take a plugin (or a separate app) with tmux. In bmux it
works out of the box, with no plugin manager.

| Instead of | Built into bmux |
| --- | --- |
| tmux-sessionx, tmux-fzf (sessions) | List-first session picker with fuzzy filtering and a live preview of the running terminal |
| tmux-sessionist | Create (Ctrl-a n), rename and terminate sessions from the picker, toggle to the last session (Ctrl-a p), step to the previous or next one (Ctrl-a ( / )); bare `bmux` opens a session named after the current directory |
| tmux-resurrect + tmux-continuum | Sessions, windows, splits, focus and each pane's directory saved every 5 seconds and on shutdown, restored automatically with agents reopening their conversations; [`bmux restart`](#restarts-and-upgrades) for upgrades |
| sesh, tmux-sessionizer | [Project picker](https://bmux-dev.github.io/bmux/guides/project-picker/) (Ctrl-a n, `bmux project`): zoxide's folders and your git repositories, landing in each folder's session or creating it |
| tmuxinator, tmuxp | [Layout files](https://bmux-dev.github.io/bmux/guides/layout-files/): a `.bmux.toml` declares a folder's windows, splits and pane commands, which run once you trust the file; `bmux layout save` writes one |
| vim-tmux-navigator | Vim/Neovim-aware [Ctrl-h/j/k/l](#vim-and-neovim-navigation) across bmux panes and into the sidebar; editor edges cross with one small mapping |
| tmux-pain-control | Ctrl-a \| and - splits in the focused pane's directory, Ctrl-a x close (asks first if a program is running), Ctrl-h/j/k/l focus, Ctrl-a h/j/k/l resize that repeats with plain h/j/k/l, Ctrl-a m zoom |
| tmux-yank | Drag, double-click or triple-click to select; copied on release with pbcopy, wl-copy, xclip, xsel, or OSC 52 over SSH. Apps' own OSC 52 copies (Neovim yanks) go to the same clipboard |
| tmux-better-mouse-mode | Click to focus, drag dividers to resize, wheel scrolls history without copy mode, hold Alt to select or scroll in mouse-aware apps |
| tmux-copycat, copy mode | An optional [scrollback view](https://bmux-dev.github.io/bmux/guides/selection-scrollback/#keyboard-scrollback) (Ctrl-a v) with vi keys, search (Ctrl-a /), and jumps between shell prompts; the mouse never needs it |
| (no tmux equivalent) | Images programs draw in a pane (an agent sharing a screenshot) with the Kitty graphics protocol, kept per pane so they survive scrolling, splits, windows and detaching; see [images](https://bmux-dev.github.io/bmux/reference/troubleshooting/#images-in-panes) |
| tmux-notify | A sound when an agent you aren't watching finishes or blocks; bells and OSC 9/777 notifications mark their pane in the sidebar |
| tmux-prefix-highlight | The status line lists the prefix keys while Ctrl-a is pending and shows when repeat resizing is active |
| tmux-powerline, tmux themes | Powerline status bars, accent colors and a light theme |
| herdr (separate app) | [Agent sidebar](#agents): every coding agent across sessions with working, waiting and blocked state |

The picker starts as a list: browse first, press **/** to search. See what's
running before attaching. Mouse selection and scrolling work without entering
copy mode. Split layouts and running shells stay available when you detach.

For tmux or Zellij users, bmux focuses on list-first session switching and
modeless mouse selection.

## Quick start

Requires macOS or Linux and an interactive terminal. Install the latest release
into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/bmux-dev/bmux/main/install.sh | sh
bmux
```

The script checks the download against the release's checksums and never uses
`sudo`; see [installing](https://bmux-dev.github.io/bmux/start/installation/) for its options. With
**Go 1.26+**, `go install github.com/josephferrero/bmux/cmd/bmux@latest` works
too (put `$(go env GOPATH)/bin` on your `PATH`).

`bmux` creates a fresh session in the current directory, named after that
directory. The server starts automatically. Existing sessions keep running.
The first time, a welcome screen covers the basic keys and leads into the
optional [agent integrations](#agents); it isn't shown again.

Press **Ctrl-a d** to detach. Return with:

```sh
bmux attach
```

This attaches to the most recently used session, including after
`bmux restart`. Running bare `bmux` again always creates another session. Use
`bmux list` to see sessions, or `bmux attach NAME` to choose one.
`bmux kill NAME` terminates a session and all its panes.

**Coming from tmux:** the prefix is **Ctrl-a**; set `prefix = "C-b"` in the
[config file](#configuration) for tmux's. Splits use **|** and **-**, as in many
tmux configs. Every key can be [rebound](https://bmux-dev.github.io/bmux/configuration/rebinding-keys/),
including giving Ctrl-l back to the shell.

## Keys

`Ctrl-a s` means press **Ctrl-a**, release, then press **s**.

| Action | Keys or mouse |
| --- | --- |
| Session picker | Ctrl-a s, or click the bottom status line |
| New-session popup (find a project or create a session) | Ctrl-a n, or `bmux project [DIR]` |
| Detach | Ctrl-a d |
| Last session | Ctrl-a p |
| Next / previous session | Ctrl-a ) / Ctrl-a ( |
| New window / select 1–9 | Ctrl-a c / Ctrl-a 1…9, or click its tab on the top bar |
| Next / previous / last window | Ctrl-a ] / Ctrl-a [ / Ctrl-a Tab |
| Rename / close window | Ctrl-a , / Ctrl-a & (asks first if a program is running) |
| Split side by side / stacked | Ctrl-a \| / Ctrl-a - |
| Close pane | Ctrl-a x (asks first if a program is running) |
| Last pane / swap / rotate | Ctrl-a ; / Ctrl-a { } / Ctrl-a Ctrl-o |
| Pane to new window / into last window | Ctrl-a ! / Ctrl-a @ |
| Preset layouts / even out splits | Ctrl-a Space (or Alt-1…5) / Ctrl-a E |
| Focus pane | Ctrl-h/j/k/l, Ctrl-a arrows, click, or Ctrl-a o to cycle |
| Sidebar | Ctrl-h from the leftmost pane; j/k select, Enter or l opens, d deletes a session, Esc returns |
| Show / hide sidebar | Ctrl-a b |
| Reload config | Ctrl-a r |
| List key bindings | Ctrl-a ? |
| Agent integrations | Ctrl-a i; Enter installs every agent found |
| Resize divider | Ctrl-a h/j/k/l, repeat h/j/k/l within one second; or drag it with the mouse |
| Select and copy | Drag and release; Alt overrides mouse-aware apps. Or Ctrl-a v, move, v/V/Ctrl-v, y |
| Scroll history | Mouse wheel or Shift-PageUp/PageDown |
| Search history | Ctrl-a /, then n/N for the next match |
| Paste | Your terminal's paste shortcut |
| Send Ctrl-a to the application | Ctrl-a Ctrl-a |

**Last session** toggles back to the session you came from. **Ctrl-h/j/k/l**
navigate panes outside Vim/Neovim; inside those editors, the keys are forwarded.
Other programs don't receive these shortcuts, including Ctrl-l to clear a shell.

In the picker, **j/k** or arrows browse, **Enter** attaches, **n** creates,
**r** renames, and **d** asks to terminate. **/** starts filtering; **Enter**
finishes the filter, then **Enter** attaches.

The [user guide](https://bmux-dev.github.io/bmux/) covers all controls, clipboard setup,
[server shutdown](https://bmux-dev.github.io/bmux/reference/runtime-files/#stop-the-server), and [uninstalling](https://bmux-dev.github.io/bmux/reference/runtime-files/#uninstall).

## Vim and Neovim navigation

bmux has vim-tmux-navigator built in: **Ctrl-h/j/k/l** move between panes,
and from the leftmost pane **Ctrl-h** continues into the sidebar. bmux checks
the pane's foreground process; when it is Vim or Neovim, the keys go to the
editor instead, so they still move between editor splits.

To cross from an editor's edge split into the next bmux pane with the same
keys, add one small mapping to `init.lua` or `.vimrc`, with no plugin and no plugin
manager. It tries an editor split first, then calls `bmux focus DIRECTION`.
The same mapping works in tmux. Copy it from
[Vim and Neovim navigation](https://bmux-dev.github.io/bmux/guides/editor-navigation/).

Outside an editor bmux keeps these keys, so by default **Ctrl-l doesn't clear
the shell**. Set `"C-l" = "none"` under `[keys.root]` to give it back, or list
programs such as fzf in `keys.pass-through` to forward the keys to them as to
Vim. **Ctrl-a arrows**, **Ctrl-a o** and clicks always move between bmux panes
directly, even with an editor in front.

Focus events (`?1004`) work with no `focus-events on` setting: Neovim's
`FocusGained`, `FocusLost` and `autoread` fire when you move between panes,
switch terminal windows, open the picker or detach.

## Agents

Run claude, codex, opencode, omp, pi, gemini or another coding agent in any
pane; bmux recognizes it from the pane's foreground process, with no setup.
The sidebar on the left lists your sessions, then every agent across them:

```
 SESSIONS              │
 ● api                 │
   web                 │
───────────────────────│
 AGENTS 2              │
 ● claude      waiting │
   api                 │
 ○ opencode       idle │
   web                 │
```

| Icon | State |
| --- | --- |
| yellow ● | working: still drawing output |
| teal ● | waiting: went quiet while you weren't looking; it finished or is asking you something |
| red ● | blocked: the agent [reported](https://bmux-dev.github.io/bmux/agents/reporting-state/) it needs a decision |
| green ○ | idle: ready, and you've seen it |

Press **Ctrl-h** from the leftmost pane to move into the sidebar, **j/k** to
pick an agent, and **Enter** or **l** to jump to its pane, in whichever session
it is. A sound plays when an agent you aren't looking at becomes ready or
blocked (`BMUX_SOUND=0` mutes it). Any program's bell or desktop notification
marks its pane too. **Ctrl-a b** hides the sidebar; it hides itself below 80
columns. Icons and colors follow herdr's defaults.

bmux never reads agent state from the screen's text. For exact state, such as
telling a permission prompt from a finished turn, install an agent's
integration: it makes the agent report its own state and session. Press
**Ctrl-a i** for the integrations panel, or use the command line:

```sh
bmux integration status          # what's installed, and what's on your PATH
bmux integration install         # every agent found, or name some: install claude
bmux integration uninstall codex
```

Integrations exist for Claude Code, Codex, omp, opencode and pi; they add
hooks, an extension or a plugin to the agent's own config, keep your other
settings and only run inside bmux panes. Any other agent can report with
`bmux agent working|blocked|idle`; see
[reporting agent state](https://bmux-dev.github.io/bmux/agents/reporting-state/).

Agents can drive bmux too. `bmux agents`, `bmux capture`, `bmux send` and
`bmux wait-agent` list agent panes, read what they ask, answer them and wait
until one needs you; `bmux guide` teaches an agent all of it, so one agent can
supervise the others and report to you. See
[scripting panes and agents](https://bmux-dev.github.io/bmux/agents/scripting/).

## Restarts and upgrades

bmux saves every session every few seconds and when the server stops. After an
upgrade, a crash or a reboot, the next server brings back:

- every session with its name, split layout and focused pane;
- a fresh shell in each pane, in the directory that pane was in;
- each agent, reopening its latest conversation in that directory
  (`claude --continue`, `codex resume --last`, `opencode --continue`,
  `omp --continue`, `pi --continue`), or the exact session its hooks reported.

Scrollback and other running programs don't come back: restored panes are new
processes, and resumed agents reopen their saved conversation rather than
continuing the process that was running.

When a newer release is out, a small **↑** appears at the right end of the
status bar (`↑ 0.6.0` when there's room). To upgrade:

```sh
bmux update     # downloads the release and checks it against its checksums
bmux restart
```

`bmux update` replaces the binary only; `bmux restart` switches the server to
it, so you choose when programs running in panes end. Until then the hint
shows **↻**. bmux looks for releases once a day; `[update] check = false` in
the config file turns that off.

`bmux restart` saves, stops the old server, starts the new one and restores your sessions.
Attached clients reconnect on their own. It works from inside a pane too. See
[Restarts and reboots](https://bmux-dev.github.io/bmux/guides/restarts/) for details and where
sessions are saved.

## Configuration

Everything works without a config file. To change settings, create
`~/.config/bmux/config.toml`. It's laid out like Helix's: a theme and the
prefix at the top, one table per area, kebab-case keys, and only the lines you
change. `bmux default-config` prints every setting with its default.

```toml
theme = "light"
prefix = "C-b"            # tmux's prefix instead of Ctrl-a

[ui]
accent = "amber"

[sidebar]
width = 30

[agents]
extra = ["myagent"]       # more programs to show as agents
waiting-after = "5s"      # quiet this long before an agent counts as waiting

[restore.resume]
claude = ["claude", "--continue", "--model", "opus"]

[keys.root]               # keys that work without the prefix
"C-l" = "none"            # Ctrl-l clears the shell again

[keys.prefix]             # keys after the prefix
"v" = "split-right"
"minus" = "split-down"
```

Apply changes with **Ctrl-a r**, `bmux reload`, or `SIGUSR1` to the server, as
in Helix; attached clients pick them up. Mistakes are shown in the status line
and that setting keeps its default. The file also covers sounds, agent state
colors, the terminal window title (session and pane title, on by default),
turning agent detection or restore off, whether to show images (`ui.graphics`), and per-agent resume commands; see
[configuration](https://bmux-dev.github.io/bmux/configuration/config-file/).

The `BMUX_THEME`, `BMUX_ACCENT`, `BMUX_POWERLINE_ASCII`, `BMUX_TERMINAL_PADDING`
and `BMUX_SOUND` environment variables still work and override the file.

## Status

This is an early prototype. CI runs on macOS and Ubuntu; it's used day to day
on macOS and Linux.

See the [roadmap](ROADMAP.md) for what's next.

## Contributing

Issues and pull requests are welcome. For bugs, include your OS, terminal
emulator, `bmux version`, and steps to reproduce.

Build from a checkout and run the checks:

```sh
go build -o bin/bmux ./cmd/bmux
./bin/bmux
go test ./...
go test -race ./...
go vet ./...
```

Read [AGENTS.md](AGENTS.md) and the [architecture guide](https://bmux-dev.github.io/bmux/project/architecture/)
before changing terminal or session behavior.

Releases are built by [GoReleaser](.goreleaser.yaml). Pushing a `vX.Y.Z` tag
runs CI, then uploads the archives and `checksums.txt` to a draft release;
write the notes and publish it. The version comes from the tag, so a release
commit only updates the README and ROADMAP. `goreleaser release --snapshot
--clean` builds the same archives locally into `dist/`.

## License

[MIT](LICENSE)
