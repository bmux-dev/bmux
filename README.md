# bmux

**The batteries-included terminal multiplexer.**

[![CI](https://github.com/josephferrero/bmux/actions/workflows/ci.yml/badge.svg)](https://github.com/josephferrero/bmux/actions/workflows/ci.yml)

bmux brings common tmux-plugin workflows into one program. Browse sessions with
live previews, copy text with the mouse, and navigate split panes without a
plugin manager.

**v0.1.1 pre-release for macOS and Linux.** Sessions survive detach and client
exit, not server exit or reboot.

<!-- TODO: demo GIF: picker with live preview → attach → split → drag to copy -->

## Built in

| Familiar tmux workflow | In bmux |
| --- | --- |
| tmux-sessionx-style picking | List-first session picker, fuzzy filtering, live previews |
| tmux-yank-style copying | Drag to select, release to copy to your clipboard |
| vim-tmux-navigator-style navigation | Vim/Neovim-aware Ctrl-h/j/k/l, with edge-crossing via a [small editor mapping](docs/editor-navigation.md) |
| Powerline themes | Built-in status bars, accent colors, and a light theme |

The picker starts as a list: browse first, press **/** to search. See what's
running before attaching. Mouse selection and scrolling work without entering
copy mode. Split layouts and running shells stay available when you detach.

For tmux or Zellij users, bmux focuses on list-first session switching and
modeless mouse selection.

## Quick start

Requires **Go 1.26+** and an interactive terminal. Put Go's binary directory on
your `PATH`: `GOBIN` if set, otherwise `$(go env GOPATH)/bin` (usually `~/go/bin`).

```sh
go install github.com/josephferrero/bmux/cmd/bmux@latest
bmux
```

`bmux` creates a fresh session in the current directory, named after that
directory. The server starts automatically. Existing sessions keep running.

Press **Ctrl-a d** to detach. Return with:

```sh
bmux attach
```

This attaches to the most recently used session. Use `bmux list` to see sessions,
or `bmux attach NAME` to choose one. `bmux kill NAME` terminates a session and
all its panes.

**Coming from tmux:** the prefix is **Ctrl-a**. Splits use **|** and **-**, as in
many tmux configs. There is no config file or key rebinding yet; both are planned.

## Keys

`Ctrl-a s` means press **Ctrl-a**, release, then press **s**.

| Action | Keys or mouse |
| --- | --- |
| Session picker | Ctrl-a s, or click the bottom status line |
| Detach | Ctrl-a d |
| Create session | Ctrl-a n |
| Last session | Ctrl-a p |
| Split side by side / stacked | Ctrl-a \| / Ctrl-a - |
| Focus pane | Ctrl-a arrows, click, or Ctrl-a o to cycle |
| Resize divider | Ctrl-a h/j/k/l; repeat h/j/k/l within one second |
| Zoom / unzoom | Ctrl-a m |
| Select and copy | Drag and release; Alt overrides mouse-aware apps |
| Scroll history | Mouse wheel or Shift-PageUp/PageDown |
| Paste | Your terminal's paste shortcut |
| Send Ctrl-a to the application | Ctrl-a Ctrl-a |

**Last session** toggles back to the session you came from. **Ctrl-h/j/k/l**
navigate panes outside Vim/Neovim; inside those editors, the keys are forwarded.
Other programs don't receive these shortcuts, including Ctrl-l to clear a shell.

In the picker, **j/k** or arrows browse, **Enter** attaches, **n** creates,
**r** renames, and **d** asks to terminate. **/** starts filtering; **Enter**
finishes the filter, then **Enter** attaches.

The [user guide](docs/usage.md) covers all controls, clipboard setup,
[server shutdown](docs/usage.md#stop-the-server), and [uninstalling](docs/usage.md#uninstall).

## Configuration

Settings are environment variables for now:

| Variable | Setting |
| --- | --- |
| `BMUX_ACCENT` | `cyan` (default), `amber`, `green`, or `#RRGGBB` |
| `BMUX_THEME` | `light`; dark by default |
| `BMUX_POWERLINE_ASCII` | `1` for plain-text separators |
| `BMUX_TERMINAL_PADDING` | `1` for physical-terminal padding |
| `BMUX_RUNTIME_DIR` | Override the server's runtime directory |

```sh
BMUX_THEME=light BMUX_ACCENT=amber bmux
```

See [configuration details](docs/usage.md#environment-variables) for defaults and
runtime paths.

## Status

This is an early prototype. CI runs on macOS and Ubuntu; day-to-day use so far
is mostly macOS.

- No windows/tabs or dedicated close-pane key yet. Use `exit` to close a shell pane.
- Scrollback does not reflow on resize. Keyboard selection and history search are planned.
- Restore after reboot is planned. There is no saved-workspace restore today.

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

Read [AGENTS.md](AGENTS.md) and the [architecture guide](docs/architecture.md)
before changing terminal or session behavior.

## License

[MIT](LICENSE)
