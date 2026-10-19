# User guide

[README](../README.md) · [Editor navigation](editor-navigation.md) · [Roadmap](../ROADMAP.md)

## Install and start

Requires Go 1.26+ and an interactive terminal on macOS or Linux.

```sh
go install github.com/josephferrero/bmux/cmd/bmux@latest
bmux
```

Go's binary directory must be on `PATH`. Check `go env GOBIN`; when empty, use
`$(go env GOPATH)/bin`, usually `~/go/bin`.

The server starts automatically. Sessions survive detach and client exit. When
the server stops or the machine reboots, the next server restores them: see
[Restarts and reboots](#restarts-and-reboots).

## Commands

| Command | Behavior |
| --- | --- |
| `bmux` | Create and attach to a fresh session in the current directory |
| `bmux new [--dir PATH] [--detach] NAME` | Create a named session; attach unless `--detach` is set |
| `bmux attach [NAME]` | Attach by name, or to the first session in `bmux list` |
| `bmux list` | Show sessions, state, pane/client counts, and starting directories, most recently used first |
| `bmux rename OLD NEW` | Rename a session |
| `bmux kill NAME` | Terminate the session and all its panes, without a confirmation prompt |
| `bmux focus DIRECTION` | Move from the calling pane toward `left`, `right`, `up`, or `down`; used by editor mappings |
| `bmux agent STATE [NAME] [-- COMMAND...]` | Report the calling pane's agent as `working`, `blocked`, `idle`, or `clear`, optionally with the command that resumes its conversation; used by agent hooks |
| `bmux restart` | Save sessions, stop the server and start this binary's server, which restores them; attached clients reconnect |
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

### Session names

Names accept Unicode letters and digits, dots, dashes, and underscores. They
must be 1–80 bytes. Explicit names must be unique; spaces are not allowed.

Bare `bmux` derives a name from the current directory:

- Replace spaces and unsupported characters with dashes, then trim dashes
  from both ends.
- Use `session` if the result is empty, `.` or `..`.
- Add a numeric suffix when needed: `api`, `api-2`, `api-3`.
- Shorten long names to fit 80 bytes, including the suffix.

Bare `bmux` always creates fresh work. It never reattaches to a matching name.

## Session picker

Open with **Ctrl-a s** or click the bottom status line. The picker starts in
list navigation, not search. A live preview appears below the list. Clicking a
row selects it for preview; it does not attach.

| Action in the list | Control |
| --- | --- |
| Move down / up | j/k, Down/Up, Ctrl-j/Ctrl-k, or mouse wheel over the list |
| Select a row | Click |
| Attach | Enter |
| Create | n or Ctrl-n |
| Rename selected session | r or Ctrl-r |
| Terminate selected session | d, then lowercase y to confirm |
| Start or edit a filter | / |
| Clear a filter, then return to the attached session | Esc; press again after clearing |
| Exit the client, leaving sessions running | Ctrl-c |

With no attached session and no filter, Esc exits the client.

### Search and filter

Press **/**, then type to fuzzy-match session names and starting directories.
While editing, letters such as j, k, n, and r are search text.

- **Up/Down** select matches while editing.
- **Backspace** or **Ctrl-h** deletes the last character; **Ctrl-u** clears the text.
- **Enter** finishes editing and keeps the filter. Use j/k to browse the results,
  then **Enter** again to attach.
- **Esc** cancels editing and clears the filter.
- **Ctrl-c** exits the client.

### Create, rename, and delete

**Create:** press n, confirm a name with Enter, then confirm the starting
directory. The name starts with the filter text or the directory name from
which this client was launched. The directory starts with that client's launch
directory. Relative paths use that directory; `~` and `~/...` are accepted.

**Rename:** press r, edit the selected session's name, and press Enter.

In either prompt, **Ctrl-u** clears the prefilled text, **Backspace/Ctrl-h**
deletes a character, and **Esc** cancels. Paste works in these fields and in
the search filter. Invalid entries show an error so you can correct them.

**Delete:** press d to see the session name, pane count, and attached clients.
Only unmodified lowercase **y** confirms termination. **n**, **Esc**, **Enter**,
and other keys cancel. Pasting text does not confirm. Confirming ends the
session and all its panes. Progress and errors appear in the status line;
the picker stays usable. Only one termination can be pending in a client.

### Preview

The preview is read-only and uses your terminal's font and the application's
text and colors. Browsing a preview does not attach, resize the application,
or move its pane focus. Large screens are cropped around the cursor rather
than shrunk. Terminals smaller than 30 columns or 16 rows show only the list.
Press Enter to attach before interacting with the application.

## Keys inside a session

The prefix is **Ctrl-a** (`prefix` in the [config file](#configuration-file)
changes it; these docs say Ctrl-a). Release it before pressing the next key.

| Action | Control |
| --- | --- |
| Open picker | Ctrl-a s, or click the bottom status line |
| Detach, leaving sessions running | Ctrl-a d |
| Create a session | Ctrl-a n |
| Toggle to the last session you came from | Ctrl-a p |
| Next / previous session, in sidebar order | Ctrl-a ) / Ctrl-a ( |
| Close the focused pane | Ctrl-a x |
| Split side by side | Ctrl-a \| |
| Split stacked | Ctrl-a - |
| Focus left/down/up/right | Ctrl-h/j/k/l outside Vim/Neovim |
| Focus the sidebar | Ctrl-h from the leftmost pane |
| Show / hide the sidebar | Ctrl-a b |
| Reload the config file | Ctrl-a r |
| Focus a neighboring pane directly | Ctrl-a arrow keys |
| Cycle visible panes | Ctrl-a o |
| Focus a pane | Click it |
| Move the nearest divider five cells | Ctrl-a h/j/k/l |
| Zoom / unzoom the focused pane | Ctrl-a m |
| Send Ctrl-a to the application | Ctrl-a Ctrl-a |
| Copy the selection again | Ctrl-a y |
| Copy the selection, if the terminal delivers this shortcut | Ctrl-Shift-c |
| Clear the selection | Esc, typing, or pasting |
| Scroll history up / down ten lines | Shift-PageUp / Shift-PageDown |
| Paste | Your terminal's paste shortcut, such as Cmd-V |

Ctrl-a p is a last-session toggle for this client, not previous-in-list
navigation. It does nothing until you have switched from another session.
Ctrl-a ) and ( step through sessions in the sidebar's order (creation order)
and wrap around.

Outside Vim/Neovim, bmux takes Ctrl-h/j/k/l, including in other programs such
as fzf, so by default Ctrl-l doesn't clear a shell. Inside Vim/Neovim they are
forwarded. See [editor navigation](editor-navigation.md) for mappings that
cross editor edges. To forward them to other programs too, list them in
`keys.pass-through` (like vim-tmux-navigator's `is_vim` pattern); to give a
key back everywhere, [unbind it](#rebinding-keys).

### Repeat resizing

After **Ctrl-a h/j/k/l**, plain **h/j/k/l** continue resizing in any direction.
Each press renews a **one-second** window. A one-second pause, another key,
a mouse click or wheel event, paste, opening the picker, or losing terminal
focus ends repetition. The footer shows when resizing is active. Unzoom
before resizing dividers.

## Agents and the sidebar

The sidebar runs down the left side of the attached view, on the bar
background. Its top section lists every session in creation order, marked
with the most urgent state inside it. Below a divider, the **Agents** section
lists every agent in every session, each with its state and session name, plus
any other pane waiting on you. Terminals narrower than 80 columns hide it;
Ctrl-a b hides or shows it at any width that fits.

bmux recognizes an agent when it is the foreground program of a pane:
aider, amp, claude, codex, copilot, crush, cursor-agent, droid, gemini, goose,
grok, kimi, kiro-cli, omp, opencode, pi and qwen, including script-based ones
started through node, bun, deno or python. Without any setup, an agent that
keeps drawing (spinners, streamed output) for about two seconds is **working**,
and one that then stays quiet for about two seconds is **waiting**. Output right
after your typing, a focus change or a resize doesn't count. bmux never reads
the screen's text.

| Mark | Meaning |
| --- | --- |
| ● red / `blocked` | An agent reported it needs a decision |
| ● teal / `waiting` | An agent stopped working while you weren't looking at its pane: it finished, or it is asking you something |
| ● teal / `bell` | A program rang the bell or sent a desktop notification (OSC 9 or OSC 777) while you weren't looking |
| ● yellow / `working` | An agent is working |
| ○ green / `idle` | An agent is ready for input and you have seen it |

Icons and colors match herdr's defaults. A session row shows its most urgent
agent: blocked, then waiting, then working, then idle.

A pane counts as seen while it is the focused pane of a session attached in
a terminal window that has focus. Focusing it clears **waiting**. **blocked**
stays until the agent reports another state. Rows disappear when the agent
exits.

When an agent you aren't looking at becomes ready (waiting) or blocked, the
attached client plays a sound: on macOS the system Glass sound, or Funk when
blocked; elsewhere the outer terminal's bell. Plain terminal bells from other
programs mark their pane silently. `sound.enabled = false` in the
[config file](#configuration-file) or `BMUX_SOUND=0` turns sounds off;
`sound.ready` and `sound.blocked` choose your own sound files.

| Action in the sidebar | Control |
| --- | --- |
| Enter the sidebar | Ctrl-h from the leftmost pane, including from Vim/Neovim via the [editor mapping](editor-navigation.md) |
| Move down / up | j/k, Ctrl-j/Ctrl-k, or Down/Up; moving past the last session continues into Agents |
| Open the selected row | Enter, l, Ctrl-l, Right, or click a row |
| Back to the panes without switching | Esc, or click a pane |
| Create a session | n |
| Prefix command | Ctrl-a, as usual |

Opening an agent row attaches its session if needed and focuses that pane.
Opening a session row focuses its first pane that is blocked or waiting on
you, if any. Keys typed in the sidebar never reach applications.

### Reporting agent state

Output timing can't tell a finished turn from a permission prompt. Agents, or
hooks they run, can report exact state by calling `bmux agent` from inside
their pane; `BMUX_SESSION_ID` and `BMUX_PANE` identify the pane. Reports replace
output timing for that pane. Report `working` when a turn starts, `idle` when
it ends, `blocked` when the agent needs a decision, and `clear` when it exits.
The optional name (at most 40 bytes) labels the sidebar row and persists
across reports. A report never starts a server and fails outside a bmux pane.
Reports from a recognized agent are dropped when it leaves the foreground.
Agents bmux doesn't recognize can still report themselves.

After `--`, a report can include the command that reopens this conversation,
such as `bmux agent idle claude -- claude --resume "$SESSION_ID"`. It replaces
the built-in resume command when the server restarts. The first word must be
a command name on `PATH`; at most 64 arguments and 8 KiB, no control
characters.

For Claude Code, add hooks to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "bmux agent working claude 2>/dev/null || true" }] }],
    "Notification": [{ "hooks": [{ "type": "command", "command": "bmux agent blocked claude 2>/dev/null || true" }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "bmux agent idle claude 2>/dev/null || true" }] }],
    "SessionEnd": [{ "hooks": [{ "type": "command", "command": "bmux agent clear 2>/dev/null || true" }] }]
  }
}
```

Agents without hooks also mark their pane when they ring the bell or send a
terminal notification. An unrecognized agent that exits without reporting
`clear` keeps its last state until something in that pane reports again.

## Panes and session lifetime

Each pane starts a login shell. The server chooses `$SHELL` when it starts,
falling back to `/bin/sh`. A new split starts in the **focused pane's current
directory**, like tmux's `-c "#{pane_current_path}"`: the directory of the
program in front of the shell (an editor started in a project, say), otherwise
the shell's. If that directory was removed, the split starts in the session's
original directory. Nested split layouts and pane focus survive detach and
reattach.

- **Ctrl-a x** closes the focused pane and stops its jobs, like tmux's
  `kill-pane`. If only the shell is running it closes at once; if a program is
  in front of the shell, the status line names it and only a lowercase **y**
  closes the pane. Any other key or a paste keeps it.
- Run `exit`, or use Ctrl-d at an empty shell prompt, to close a shell pane.
- When the shell exits, the pane closes and its sibling expands. This also
  happens while detached.
- Closing the last pane removes the session and returns attached clients to
  the picker.
- A session supports up to 32 panes. Splitting requires enough space; dividers
  stop at minimum pane sizes.
- Shrinking the terminal can hide panes. Zoom the focused pane to work in a
  smaller window. Moving focus in a direction unzooms, like tmux.

Several clients can attach to one session. Pane focus is shared. The latest
attachment or attached-client resize sets the shared application size.

Applications that ask for focus events (`?1004`), such as Neovim for
`FocusGained`, `FocusLost` and `autoread`, hear that their pane has focus while
it is the session's focused pane and at least one attached client's terminal
window has focus. Moving to another pane, switching terminal windows, opening
the picker and detaching all report focus out. An application that turns
focus events on while focused is told at once, as in tmux.

Terminating a pane or session also stops its ordinary jobs. Programs that
deliberately detach themselves from the shell's process session can keep running.

## Restarts and reboots

The server saves every session every few seconds and when it stops cleanly
(`bmux restart`, SIGTERM or SIGINT). The next server, whether after an upgrade,
a crash or a reboot, restores them before accepting clients:

- the same sessions and names, split layout, focused pane and zoom;
- a fresh login shell in each pane, in the directory that pane's shell was in;
- agents, by typing a resume command into the restored shell. Reported
  commands run as given. Otherwise recognized agents continue their latest
  conversation in that directory: `claude --continue`, `codex resume --last`,
  `opencode --continue`, `omp --continue`, `pi --continue`. When two panes
  would continue the same agent in the same directory, only the first does;
  the other gets a shell, so one conversation never opens twice.

Nothing else that was running comes back: no scrollback, no editors, servers
or other programs. Resumed agents are new processes reopening their saved
conversation, not the processes that were running.

`bmux restart` from inside a pane finishes in the background, because stopping
the server ends that pane's jobs. Attached clients reconnect to the session
they showed once the new server is up.

Sessions are saved to `~/Library/Application Support/bmux/sessions.json` on
macOS and `${XDG_STATE_HOME:-~/.local/state}/bmux/sessions.json` on Linux, or
`sessions.json` inside a custom `BMUX_RUNTIME_DIR`, so separate servers keep
separate sessions. A file bmux can't read is moved to `sessions.json.unreadable`
and the server starts without restoring.

## Mouse selection and scrollback

| Action | Mouse |
| --- | --- |
| Select and copy | Drag, then release |
| Select a word and copy | Double-click |
| Select a line and copy | Triple-click |
| Scroll history | Wheel up/down, three lines per event |
| Select or scroll history in a mouse-aware application | Hold Alt |
| Resize panes | Drag a divider between them |

Dragging a divider moves it as you go, down to each pane's minimum size,
without changing which pane is focused. Where dividers meet, the one running
through the junction moves. A zoomed pane has no dividers to drag.

Mouse-aware applications receive mouse events by default. **Alt** gives the
gesture to bmux instead. If your terminal reserves Shift-drag, it performs
the terminal's own selection; if it passes Shift through, bmux handles selection.

Typing or pasting returns to live output. Each pane keeps up to **10,000 lines**
of scrollback. Full-screen alternate-screen applications use their own scrolling;
bmux history scrolling is unavailable there.

Selections survive pane focus changes. Resizing a pane, repainting selected
text, or removing it from history clears the selection. Dragging beyond the
view does not auto-scroll yet. Scrollback does not reflow on resize, and copied
soft-wrapped lines currently include newlines. Wide, busy histories use more
memory. Keyboard selection and history search are planned.

## Clipboard and paste

bmux highlights selections and copies them on mouse release. Clipboard access
happens on the machine running the attached client.

| Environment | Clipboard method |
| --- | --- |
| Local macOS | `pbcopy` |
| Local Linux, Wayland | `wl-copy` when `WAYLAND_DISPLAY` is set |
| Local Linux, X11 | `xclip`, then `xsel` if unavailable, when `DISPLAY` is set |
| SSH, or no available desktop clipboard tool | OSC 52 request to the outer terminal |

On Linux, install a clipboard tool for your desktop and make it available on
`PATH`. If both desktop environments are set, bmux looks for wl-copy first.
A helper failure is reported in the status line.

Over SSH, bmux uses OSC 52 rather than the remote machine's desktop clipboard.
Enable OSC 52 clipboard writes in your local terminal. The status line says
the copy was **requested** because bmux cannot verify that the terminal accepted
it. Terminal-specific size limits apply.

Applications can copy too. When a program in any pane of the session sends an
OSC 52 clipboard write, such as Neovim yanking to `+` or `*` (its OSC 52
provider, also over SSH), every attached client copies the text with the
methods above, without a status message unless copying fails. Copies made
while no client was attached are not replayed later. All selection targets
(clipboard, primary, numbered) go to the clipboard. Apps can't read the
clipboard: OSC 52 queries are ignored. Writes over about 3 MiB, invalid
base64 and invalid UTF-8 are dropped.

Cmd-C usually belongs to the outer terminal; use copy-on-release or Ctrl-a y.

Paste with your terminal's normal shortcut. bmux does not read the clipboard
automatically. It respects the application's bracketed-paste setting and treats
pasted shortcuts as text, not bmux commands. The maximum paste is **4 MiB**.
Oversized, invalid UTF-8, and incomplete bracketed pastes are rejected with an
error. If the application cannot accept more input, a paste can be rejected;
wait and retry. In the picker, paste only into a name, directory, or `/` filter.

## Configuration file

bmux reads `~/.config/bmux/config.toml` (or `$XDG_CONFIG_HOME/bmux/config.toml`,
or the path in `BMUX_CONFIG`). It's TOML laid out like Helix's `config.toml`: a
theme and the prefix at the top, then one table per area with kebab-case keys.
The file and every line in it are optional; write only what you change.
`bmux default-config` prints every setting, commented out, with its default:

```toml
theme = "dark"
prefix = "C-a"

[ui]
accent = "cyan"

[sidebar]
width = 24
```

| Setting | Default | What it does |
| --- | --- | --- |
| `theme` | `"dark"` | `"dark"` or `"light"` |
| `prefix` | `"C-a"` | The command key, written like Helix keys: `C-a` to `C-z`, except `C-h/j/k/l` (navigation) and `C-i`/`C-m` (Tab and Enter). `ctrl-a` also works. Status-line help follows it. |
| `ui.accent` | `"cyan"` | `cyan`, `amber`, `green` or `"#RRGGBB"` |
| `ui.powerline-ascii` | `false` | Plain `<` `>` separators instead of powerline glyphs |
| `ui.terminal-padding` | `false` | Keep physical-terminal padding; read when a client starts |
| `ui.set-title` | `true` | Name the outer terminal's window after the session and, when its app set one, the focused pane's title (`api · nvim README.md`); `bmux` in the picker. Like tmux's `set-titles`; your terminal's own title comes back when bmux exits. Turning it off stops updates; the current title stays until then. |
| `ui.agent-colors.working` / `waiting` / `blocked` / `idle` | herdr's palette | Sidebar state colors as `"#RRGGBB"` |
| `sidebar.show` | `true` | Show the sidebar when a client starts; Ctrl-a b still toggles it |
| `sidebar.width` | `24` | Columns including its border, 12 to 60 |
| `sidebar.min-screen-width` | `80` | Hide the sidebar on narrower terminals |
| `sound.enabled` | `true` | Alert sounds for background agents |
| `sound.ready` / `sound.blocked` | system sounds | Sound files (absolute or `~/` paths), played with `afplay` on macOS or `paplay`/`aplay` on Linux |
| `agents.detect` | `true` | Recognize agents in panes; reported agents still show when off |
| `agents.extra` | `[]` | More program names to recognize as agents |
| `agents.working-after` / `agents.waiting-after` | `"2s"` / `"2s"` | How long an agent must draw before it counts as working, and stay quiet before it counts as waiting (1s to 1m) |
| `restore.enabled` | `true` | Save and restore sessions. When false, nothing is restored or saved and the existing save file is left alone. |
| `restore.resume.AGENT` | built-in | Replace an agent's resume command, e.g. `claude = ["claude", "--continue", "--model", "opus"]`; `[]` restores it as a plain shell |
| `keys.root.KEY` / `keys.prefix.KEY` | the keys above | Bind a key to an action, or `"none"`; see [Rebinding keys](#rebinding-keys) |
| `keys.pass-through` | `[]` | Programs that get Ctrl-h/j/k/l themselves, like Vim and Neovim do, e.g. `["fzf", "lazygit"]` |

**Ctrl-a r** reloads the file in your client and the server; `bmux reload` does
the same from a shell, and, like Helix, so does sending the server `SIGUSR1`
(`pkill -USR1 -f 'bmux --serve'`). Every attached client follows. If a setting
is invalid, the status line (or `bmux reload`) says which, and that setting
keeps its default, so a typo never leaves you without keys. Unknown settings
are reported too.

### Rebinding keys

`[keys.root]` holds keys that work without the prefix; `[keys.prefix]` holds
the keys you press after it. Write only the keys you change; the rest keep
their defaults (`bmux default-config` lists them). `"none"` unbinds a key:
a root key then reaches the application, and a prefix key does nothing.

```toml
[keys.root]
"C-l" = "none"            # Ctrl-l clears the shell; Ctrl-a arrows still move right
"A-h" = "navigate-left"   # Alt-h/j/k/l as well as Ctrl-h/j/k/l

[keys.prefix]
"v" = "split-right"
"s" = "split-down"
"w" = "sessions"
"H" = "resize-left"       # repeats with plain H, like h does by default
```

Keys are written like Helix's: `C-` Ctrl, `A-` Alt, `S-` Shift, then a
character or one of `left`, `right`, `up`, `down`, `home`, `end`, `pageup`,
`pagedown`, `ins`, `del`, `backspace`, `tab`, `ret`, `esc`, `space`, `minus`
(or `"-"`), `F1` to `F12`. Uppercase letters are shifted keys; Ctrl-Shift
letters are the same as Ctrl letters in most terminals.

| Action | Default key | What it does |
| --- | --- | --- |
| `navigate-left` / `-down` / `-up` / `-right` | root `C-h/j/k/l` | Focus a pane, or move between Vim/Neovim splits |
| `focus-left` / `-down` / `-up` / `-right` | prefix arrows | Focus a pane, even with an editor in front |
| `next-pane` | prefix `o` | Cycle panes |
| `resize-left` / `-down` / `-up` / `-right` | prefix `h/j/k/l` | Move a divider five cells; prefix keys bound to resize repeat without the prefix for a second |
| `split-right` / `split-down` | prefix `\|` / `-` | Split side by side / stacked |
| `zoom`, `close-pane` | prefix `m`, `x` | |
| `sessions`, `new-session`, `last-session` | prefix `s`, `n`, `p` | |
| `previous-session` / `next-session` | prefix `(` / `)` | |
| `detach`, `copy` | prefix `d`, `y` | |
| `scroll-up` / `scroll-down` | root `S-pageup` / `S-pagedown` | Ten lines of history |
| `toggle-sidebar`, `reload-config` | prefix `b`, `r` | |
| `send-prefix` | the prefix again | Send the prefix key to the application |

The prefix can't be a root key. The status line's hints and the prefix help
show your bindings.

## Environment variables

These override the config file. Set them before starting the client, or
export them in your shell startup file.

| Variable | Values and default |
| --- | --- |
| `BMUX_CONFIG` | Path of the config file |
| `BMUX_ACCENT` | Overrides `ui.accent` |
| `BMUX_THEME` | `light` selects a light palette; any other value uses dark |
| `BMUX_POWERLINE_ASCII` | `1` forces `<` and `>` separators |
| `BMUX_TERMINAL_PADDING` | `1` preserves physical-terminal padding |
| `BMUX_SOUND` | `0` turns off agent alert sounds |
| `BMUX_RUNTIME_DIR` | Custom runtime directory; default follows the rules below |

Appearance settings affect bmux's bars and highlights, not application colors.

```sh
BMUX_THEME=light BMUX_ACCENT=amber bmux
BMUX_ACCENT='#aabbcc' bmux
BMUX_POWERLINE_ASCII=1 bmux
```

Inside panes, bmux sets `BMUX_SESSION`, `BMUX_SESSION_ID` and `BMUX_PANE` for
[editor mappings](editor-navigation.md) and [agent reports](#reporting-agent-state).
These are pane information, not settings.

## Runtime files

The runtime directory is chosen in this order:

1. `BMUX_RUNTIME_DIR`, if set. Relative paths are resolved from the invoking directory.
2. `$XDG_RUNTIME_DIR/bmux`, if `XDG_RUNTIME_DIR` is set.
3. On macOS, `~/Library/Caches/bmux`.
4. On Linux, `$XDG_CACHE_HOME/bmux`, or `~/.cache/bmux` if unset.

Use an absolute path for `BMUX_RUNTIME_DIR` so clients find the same server.
The directory must belong to your user, have mode **0700**, and not be a symlink.
bmux creates it if missing. Changing the directory selects a separate server;
it does not move existing sessions.

| File | Purpose |
| --- | --- |
| `server.log` | Server output and startup errors |
| `server.pid` | Background server's process ID |
| `server.sock` | Local client/server connection |
| `server.lock` | Prevents duplicate servers in this directory |

### Stop the server

Use **Ctrl-a d** to detach and leave work running. To upgrade, use
`bmux restart` instead of stopping the server by hand; see
[Restarts and reboots](#restarts-and-reboots). A server stopped with SIGTERM
also saves its sessions first.

From a shell outside bmux, resolve the directory using the same environment
you used to launch bmux:

```sh
if [ -n "$BMUX_RUNTIME_DIR" ]; then
  runtime_dir="$BMUX_RUNTIME_DIR"
elif [ -n "$XDG_RUNTIME_DIR" ]; then
  runtime_dir="$XDG_RUNTIME_DIR/bmux"
elif [ "$(uname -s)" = Darwin ]; then
  runtime_dir="$HOME/Library/Caches/bmux"
else
  runtime_dir="${XDG_CACHE_HOME:-$HOME/.cache}/bmux"
fi

kill -TERM "$(cat "$runtime_dir/server.pid")"
```

Wait for `server.pid` to disappear before restarting. The next `bmux` command
starts a new server, which restores the saved sessions. The log remains
available for troubleshooting. To stop for good without restoring later, kill
the sessions first with `bmux kill NAME`.

### Uninstall

Stop the server first using the instructions above. Removing the executable
alone does not stop running sessions.

For a Go installation, remove the installed binary:

```sh
go_bin="$(go env GOBIN)"
if [ -z "$go_bin" ]; then
  go_bin="$(go env GOPATH)/bin"
fi
rm "$go_bin/bmux"
```

If you moved the binary or built from source, remove that executable instead;
`command -v bmux` locates the one on your `PATH`. After shutdown, you can remove
`server.log` and `server.lock` from the runtime directory. Remove any `BMUX_*`
settings you added to your shell configuration.

## Troubleshooting

### Protocol version mismatch

An upgraded client may not work with a server started by an older binary.
Run `bmux restart` with the new binary: it saves the sessions, stops the old
server, and starts the new one, which restores them. Servers from before
saving existed are stopped with SIGTERM and their sessions are not restored.
Do not delete the socket to try to upgrade a running server.

### Server does not start

Read `server.log` in the runtime directory. Check directory ownership and mode
0700. For a runtime-path-too-long error, use a shorter private directory through
`BMUX_RUNTIME_DIR`. Use the same value for every client connecting to that server.

### Backspace moves pane focus

ASCII BS (`0x08`) and Ctrl-h are the same input. Configure your terminal's
Backspace key to send **DEL (`0x7f`)** instead. Normal Enter and DEL Backspace
reach applications. See [editor navigation](editor-navigation.md) for Ctrl-h/j/k/l.

### Broken powerline separators

Run `BMUX_POWERLINE_ASCII=1 bmux` to use plain-text separators. This works even
when the terminal reports glyph support but the font renders the glyphs poorly.

### Physical terminal needs padding

Run `BMUX_TERMINAL_PADDING=1 bmux`. Modern terminal emulators normally use the
default, with padding disabled.

### Terminal compatibility

Graphics protocols and clickable hyperlinks are not supported yet. CI runs on
macOS and Ubuntu; most day-to-day interactive use has been on macOS. For a bug
report, include your OS, terminal emulator, `bmux version`, and reproduction steps.
