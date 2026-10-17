# bmux

**The batteries-included terminal multiplexer.**

[![CI](https://github.com/josephferrero/bmux/actions/workflows/ci.yml/badge.svg)](https://github.com/josephferrero/bmux/actions/workflows/ci.yml)

Persistent workspaces, a list-first session picker, and panes that feel like
terminals. Splits, session switching, Vim/Neovim pane navigation and clipboard
copy are built in—no plugin manager required.

bmux is a Go terminal multiplexer prototype for macOS and Linux. Its interaction
model is session-first and modeless: select text with the mouse, scroll through
history, and copy without entering a separate copy mode.

See [ROADMAP.md](ROADMAP.md) for what's planned on the way to tmux parity.

## Install

Requires Go 1.26 or later and an interactive terminal.

```sh
go install github.com/josephferrero/bmux/cmd/bmux@latest
bmux
```

Or from a checkout:

```sh
go build -o bin/bmux ./cmd/bmux
./bin/bmux
```

Running bare **`bmux` creates and attaches to a fresh session in the current
directory**, with no picker or naming prompt. The directory supplies the session
name; existing names get a numeric suffix (`api`, `api-2`, `api-3`). Spaces and
unsupported name characters become dashes. Existing sessions are left running.
Press **Ctrl-a s** when you want the session picker.

In the picker, **n** creates a session. Confirm its name and working
directory (Ctrl-u clears the prefilled value). Type to fuzzy-search names and
directories only after pressing **/**; the default is list navigation, not a
search field. **j/k** (or arrows) select a session; **Enter** attaches. **r**
renames, and **d** opens a centered confirmation modal over the picker. Only
lowercase **y** (Yes) terminates the session; **n** or **Esc** selects No. Enter
and other keys also cancel. The modal shows the name and pane count, plus attached
terminal clients when nonzero; it omits cleanup paragraphs and zero-client labels.
Counts refresh while the modal is open without changing its captured target.
Confirmation dismisses the modal immediately and shows progress in the bottom
status bar while the picker remains usable. Failures are reported, not silently
treated as successful deletion. Captured terminate/rename actions
validate session generation, so name reuse cannot target replacement work. Termination
hangs up then escalates signals for ordinary jobs remaining in the pane's OS process
session, including separate job-control groups. Deliberately daemonized/`setsid`-escaped
processes are outside that boundary. Cleanup has a 1.25-second grace/escalation
budget (TERM after about 0.5 seconds, KILL after about 1 second if needed), so jobs
that resist the hangup can take longer to terminate than a plain idle shell.
Ctrl-n/Ctrl-r remain aliases for new/rename.

A live, input-read-only terminal view shows the highlighted session
**below the session list**, using your terminal's actual character cells and font.
It updates up to **10 times per second** while output changes. Very small
terminals show just the list. Viewing does **not** attach or resize applications,
change pane focus, send input, or change session recency. Mouse clicks select a
row for preview; Enter attaches.

### Native terminal view

The bottom region renders the source glyphs, Unicode, colors, and attributes
directly—no PNGs, alternate fonts, Braille thumbnails, scaling, or image protocols.
Like tmux, bmux fits a **viewport**, not the application, into the panel. Split
panes retain their proportions in the presentation layout; zoomed panes fill it.
Each viewport copies native source cells around the cursor, positioned near the
top-left third when space permits. A hidden cursor uses the top-left of the source.
Large applications are cropped, not reflowed or font-scaled. The actual PTYs and
other attached clients are never resized by browsing or leaving the picker.

The first snapshot is sent immediately—no application redraw, initial refresh
tick, resize lease, or settling timer. Subsequent output updates arrive up to
10 times per second. Several clients can preview different panel sizes independently.
The cursor is shown without an invented blink timer; search and prompts retain
their own cursor. Keys and mouse actions in the picker still select sessions;
press Enter to attach and interact normally.

The picker follows tmux's visual layout: a segmented powerline status bar,
full-width numbered session rows (`(0) + name: N panes`), and a bordered native
terminal view in the lower half.
The preview border shows only the session name and list position—no sorting,
view-mode, read-only, or periodic refresh labels. Loading and actual errors remain
visible when needed. Attachment labels use real client counts;
bmux does not label panes as fictitious windows. The default palette stays neutral
with a cyan accent. Fonts without powerline separators can use
`BMUX_POWERLINE_ASCII=1 ./bin/bmux`.

Legacy hardware padding is disabled by default; physical terminals can set
`BMUX_TERMINAL_PADDING=1`.

Save work before restarting a server, since shutdown ends its ordinary owned jobs
and sessions. Client and server must run the same protocol version; a mismatched
pair is rejected rather than guessing.

Search is optional: **/** starts filtering, **Enter** finishes editing the filter,
then j/k navigate the results normally. Escape clears search/filter before
leaving the picker.

```sh
./bin/bmux new --dir ~/projects/api api
./bin/bmux new --detach logs
./bin/bmux list
./bin/bmux attach api
./bin/bmux attach  # Attach to the first session listed (most recently used)
./bin/bmux rename api backend
./bin/bmux kill logs  # explicitly terminates this session's shell
```

Flags go **before** the session name. `bmux --help` lists commands.
`bmux attach` without a name attaches directly to the first session in
`bmux list`; if none exist, it tells you to create one. Running bare `bmux`
always starts a fresh session in the current directory instead.

### Inside a session

| Action | Control |
| --- | --- |
| Session picker | Ctrl-a s, or click the bottom status line |
| Detach, leaving the shell running | Ctrl-a d |
| Create a session | Ctrl-a n |
| Previous session | Ctrl-a p |
| Split side by side / stacked | Ctrl-a \| / Ctrl-a - |
| Focus another pane | Ctrl-h/j/k/l, click, Ctrl-a arrow keys, or Ctrl-a o to cycle |
| Move the nearest divider five cells | Ctrl-a h/j/k/l, then repeat plain h/j/k/l |
| Zoom / unzoom the focused pane | Ctrl-a m |
| Send Ctrl-a to the application | Ctrl-a Ctrl-a |
| Select and copy | Click-drag, then release |
| Select a word / line | Double-click / triple-click |
| Copy selection again | Ctrl-a y |
| Clear selection | Escape, or begin typing |
| Scroll history | Mouse wheel or Shift-PageUp/PageDown |
| Paste | Your outer terminal's paste shortcut (e.g. Cmd-V) |

Typing or pasting returns to live output. Applications' cursor styles and
visibility are preserved. Mouse-aware applications receive mouse input; hold
**Alt** to select or scroll terminal history instead. Some terminals reserve
Shift-drag for their own selection, which remains an outer-terminal feature.

**Ctrl-h/j/k/l** move left/down/up/right between bmux panes at a shell. When
**Neovim or Vim is the foreground app**, bmux forwards those keys instead, so
your editor's split-navigation mappings work normally. At an editor edge, a
bmux-aware mapping hands focus back to bmux: `bmux focus left|right|up|down`
(inside a pane, bmux exposes `BMUX_SESSION` and `BMUX_PANE` like tmux does)
moves the focus to the neighboring bmux pane. Foreground detection uses the
PTY's OS process group—not terminal titles—and shell navigation resumes when
the editor exits or is suspended. Use **Ctrl-a + arrows**, a mouse click, or
**Ctrl-a o** to explicitly move between bmux panes while in an editor.
While a session is zoomed, edge crossing is a no-op, matching tmux's
`disable_when_zoomed`. Enter and ordinary Backspace continue to reach the
application.
On terminals configured to send ASCII BS (`0x08`) for Backspace, it is inherently
indistinguishable from Ctrl-h; configure Backspace to send DEL (`0x7f`) instead.

After **Ctrl-a h/j/k/l**, plain **h/j/k/l** keep resizing in any direction.
Each press renews a **one-second** repeat window. A one-second pause, any other
key, mouse interaction, paste, or opening the picker ends repetition. Once it
ends, letters go to the application normally. The footer indicates repeat resizing.

Splits are built in—no plugins needed. Each pane owns its own shell, scrollback,
selection and cursor. Nested split layouts and the focused pane survive
detach/reattach. New panes start in the session's original working directory.
Selections survive pane focus changes, but are cleared when their pane resizes
so highlighting and copied text cannot refer to stale cell positions.
When a pane's shell exits (including `exit` or Ctrl-d at an empty shell prompt),
the pane closes automatically and its sibling expands to fill the space. Focus
moves to a surviving neighboring pane if needed. Closing the last pane removes
the session and returns attached clients to the session picker. This also works
while detached; an exited shell does not leave an empty pane behind. There is
not yet an explicit close-pane shortcut. Split creation is rejected when there isn't room,
and dividers cannot resize panes below their minimum dimensions. If the terminal
shrinks below the whole layout's minimum size, panes can become hidden; zoom the
focused pane to work in the smaller terminal.

### Clipboard

Selection is highlighted by bmux and automatically copied on release. The
**attached client**, not the session server, owns clipboard operations:

- macOS: `pbcopy` writes directly to the system clipboard.
- Linux: `wl-copy`, `xclip`, or `xsel`, depending on the desktop environment.
- SSH or no desktop clipboard tool: request a clipboard write using OSC 52.
  bmux labels this as a **request**, not verified success; the terminal must
  permit OSC 52. Terminal-specific size limits may apply.

Normal terminal paste is forwarded using the application's bracketed-paste
setting. bmux doesn't read your clipboard automatically. Cmd-C is generally
handled by the outer terminal and cannot be guaranteed to reach a CLI app;
copy-on-release avoids depending on it. Application-originated OSC 52 writes
(for example from Neovim) are not forwarded in this prototype.

Bracketed-paste bytes are captured before key decoding, preserving BS/DEL,
embedded CSI, Unicode, and CR/LF/CRLF without treating pasted shortcuts as commands.
Invalid UTF-8, incomplete pastes and payloads above 4 MiB are visibly rejected.
Large pastes use bounded transport chunks and reach the application as one logical
paste only after complete validation; queue pressure rejects whole requests.
Clipboard helpers run in an ordered worker off the UI thread with bounded waits.

## Architecture

```text
bmux client (picker, rendering, selection, local clipboard)
    │ private, versioned JSON protocol over a Unix socket
bmux server (PTYs, terminal screens, 10,000-line scrollback)
    └ login shell per pane, with a persistent binary split-layout tree
```

The server starts automatically and survives client exit. Terminal emulation
uses Charm's `x/vt`; `tcell` handles the outer terminal and input; `creack/pty`
manages PTYs. Screen snapshots are coalesced to at most 30 updates per second
per attached client; only the visible viewport is transmitted.

Runtime files live in `$XDG_RUNTIME_DIR/bmux`, or the OS user's cache directory
under `bmux`. Override with `BMUX_RUNTIME_DIR` (a private directory with mode
0700). The socket has mode 0600, and a lock prevents duplicate servers. Server
startup errors are written to `server.log` in that directory.

`server.pid` identifies the background server. Sending it SIGTERM shuts down
the server **and its sessions**; use detach instead to leave work running.

## Honest prototype boundaries

- Native nested splits are supported; tabs and explicit close-pane are future work.
- Sessions survive detach/client exit, **not server termination or reboot**.
  Workspace snapshots and restart restoration are planned. Arbitrary live
  processes cannot generally be resurrected after reboot.
- Neovim/Vim receive Ctrl-h/j/k/l for their own split mappings; crossing an
  editor edge into a bmux pane uses `focus-pane` requests carrying the pane's
  `BMUX_PANE` identity, plus one-shot `focus-pane` routing in the registry.
  While zoomed, edge crossing is ignored.
- Several clients can attach; the most recent attached-client resize controls
  shared PTY dimensions. Picker previews never resize the PTYs. Pane focus is shared.
  Per-client sizing/focus policy is future work.
- Scrollback isn't reflowed on resize; selection across soft-wrapped lines
  currently includes newlines. Monotonic row identities anchor retained scrollback
  after the history cap. Genuinely evicted, repainted, or reset selected cells clear
  the selection and copy source together. Selection auto-scroll while dragging is
  not implemented yet. Dense, wide histories can use substantial memory.
- Terminal-only text selection has unavoidable outer-terminal constraints.
  Graphics protocols, hyperlinks, and full terminal conformance aren't claimed.
- Linux is a build target; this first implementation is being exercised on macOS.

## Development

[Architecture/ownership contracts](docs/architecture.md) · [Roadmap](ROADMAP.md)

### Appearance

The picker uses a powerline top bar, dense numbered rows with a bold/reverse
full-width selection, explicit attachment/client counts, and a bordered live
terminal below. Small windows hide the terminal panel instead of shrinking text.

Both the picker and attached sessions keep a powerline at the top and a status
row at the bottom. Attached PTYs use the rows between these bars; chrome never
covers application text or receives application mouse input. Direction markers on reserved
divider cells identify focus without drawing over application contents. The
default accent is cyan; optional settings affect bmux chrome, not application colors:

```sh
BMUX_ACCENT=amber ./bin/bmux
BMUX_ACCENT=green ./bin/bmux
BMUX_THEME=light ./bin/bmux
```

### Checks

```sh
go test ./...
go test -race ./...
go vet ./...
```

## License

[MIT](LICENSE)
