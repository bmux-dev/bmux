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

The server starts automatically. Sessions survive detach and client exit, but
end when the server exits or the machine reboots. There is no saved-workspace
restore yet.

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

The prefix is **Ctrl-a**. Release it before pressing the next key.

| Action | Control |
| --- | --- |
| Open picker | Ctrl-a s, or click the bottom status line |
| Detach, leaving sessions running | Ctrl-a d |
| Create a session | Ctrl-a n |
| Toggle to the last session you came from | Ctrl-a p |
| Split side by side | Ctrl-a \| |
| Split stacked | Ctrl-a - |
| Focus left/down/up/right | Ctrl-h/j/k/l outside Vim/Neovim |
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

Outside Vim/Neovim, bmux takes Ctrl-h/j/k/l, including in other programs such
as fzf. These keys do not reach those programs; Ctrl-l does not clear a shell.
Inside Vim/Neovim they are forwarded. See [editor navigation](editor-navigation.md)
for mappings that cross editor edges.

### Repeat resizing

After **Ctrl-a h/j/k/l**, plain **h/j/k/l** continue resizing in any direction.
Each press renews a **one-second** window. A one-second pause, another key,
a mouse click or wheel event, paste, opening the picker, or losing terminal
focus ends repetition. The footer shows when resizing is active. Unzoom
before resizing dividers.

## Panes and session lifetime

Each pane starts a login shell. The server chooses `$SHELL` when it starts,
falling back to `/bin/sh`. New panes start in the **session's original directory**,
not the focused pane's current directory. Nested split layouts and pane focus
survive detach and reattach.

- Run `exit`, or use Ctrl-d at an empty shell prompt, to close a shell pane.
- When the shell exits, the pane closes and its sibling expands. This also
  happens while detached.
- Closing the last pane removes the session and returns attached clients to
  the picker.
- There is no dedicated close-pane shortcut yet.
- A session supports up to 32 panes. Splitting requires enough space; dividers
  stop at minimum pane sizes.
- Shrinking the terminal can hide panes. Zoom the focused pane to work in a
  smaller window. Unzoom to navigate to other panes.

Several clients can attach to one session. Pane focus is shared. The latest
attachment or attached-client resize sets the shared application size.

Terminating a pane or session also stops its ordinary jobs. Programs that
deliberately detach themselves from the shell's process session can keep running.

## Mouse selection and scrollback

| Action | Mouse |
| --- | --- |
| Select and copy | Drag, then release |
| Select a word and copy | Double-click |
| Select a line and copy | Triple-click |
| Scroll history | Wheel up/down, three lines per event |
| Select or scroll history in a mouse-aware application | Hold Alt |

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

**Application-originated OSC 52 writes are not forwarded yet**, including
Neovim clipboard writes. Mouse selection in bmux still uses the methods above.
Cmd-C usually belongs to the outer terminal; use copy-on-release or Ctrl-a y.

Paste with your terminal's normal shortcut. bmux does not read the clipboard
automatically. It respects the application's bracketed-paste setting and treats
pasted shortcuts as text, not bmux commands. The maximum paste is **4 MiB**.
Oversized, invalid UTF-8, and incomplete bracketed pastes are rejected with an
error. If the application cannot accept more input, a paste can be rejected;
wait and retry. In the picker, paste only into a name, directory, or `/` filter.

## Environment variables

There is no config file or key rebinding yet. Set variables before starting
the client, or export them in your shell startup file.

| Variable | Values and default |
| --- | --- |
| `BMUX_ACCENT` | `cyan` by default; accepts `amber`, `green`, or a six-digit `#RRGGBB` color |
| `BMUX_THEME` | `light` selects a light palette; unset or any other value uses dark |
| `BMUX_POWERLINE_ASCII` | `1` forces `<` and `>` separators; otherwise powerline glyphs with automatic fallback when unavailable |
| `BMUX_TERMINAL_PADDING` | `1` preserves physical-terminal padding; disabled by default |
| `BMUX_RUNTIME_DIR` | Custom runtime directory; default follows the rules below |

Theme and accent names are case-insensitive. Unknown accents use cyan. Appearance
settings affect bmux's bars and highlights, not application colors. Restart the
client to change appearance; the server can keep running.

```sh
BMUX_THEME=light BMUX_ACCENT=amber bmux
BMUX_ACCENT='#aabbcc' bmux
BMUX_POWERLINE_ASCII=1 bmux
```

Inside panes, bmux sets `BMUX_SESSION` and `BMUX_PANE` for
[editor mappings](editor-navigation.md). These are pane information, not settings.

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

**Stopping the server ends all its sessions and panes.** Save work first.
Use **Ctrl-a d** instead to detach and leave work running. There is no
`kill-server` command yet.

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
starts a new server. The log remains available for troubleshooting.

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
Finish and save work in existing clients, then [stop the server](#stop-the-server).
Run `bmux` again with the installed binary. Sessions do not survive this restart.
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
