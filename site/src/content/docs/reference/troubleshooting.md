---
title: "Troubleshooting"
description: "Fixes for protocol mismatches, server startup, CPU profiling, focus keys, powerline glyphs, padding and terminal compatibility."
sidebar:
  order: 3
---

## Protocol version mismatch

After `bmux update` (or any upgrade that changed the protocol), commands fail
until the server runs the new version too, and say which side is older:

```
bmux: the running bmux server is an older version (protocol 8; this bmux speaks 10). Run bmux restart …
```

Run `bmux restart` with the new binary: it saves the sessions, stops the old
server, and starts the new one, which restores them. Programs running in panes
end. When the server is the newer one, an older `bmux` is first on your
`PATH`: use the binary that started the server, or `bmux restart` with the one
you want. Servers from before v0.2, which didn't save sessions, are stopped
with SIGTERM and their sessions are not restored. Do not delete the socket to
try to upgrade a running server.

## Server does not start

Read `server.log` in the runtime directory. Check directory ownership and mode
0700. For a runtime-path-too-long error, use a shorter private directory through
`BMUX_RUNTIME_DIR`. Use the same value for every client connecting to that server.

## CPU and allocation profiles

Set `BMUX_PPROF_DIR` when starting a client or server to expose Go CPU,
allocation and heap profiles. It is **disabled by default**. Each opted-in
process creates a Unix socket named `client-PID.sock` or `server-PID.sock` in
that directory. There is no TCP listener. The directory must be owned by your
user, have mode 0700, and not be a symlink; bmux creates it privately if missing.
Sockets have mode 0600 and are removed when their process exits normally.

An already running server does not pick up this environment variable. A new
client can be profiled without restarting that server, but a server profile
requires a server started with the variable. Use an isolated test build instead
of restarting important running jobs:

```sh
# From a checkout, create a short, private test directory.
WORK=$(mktemp -d /tmp/bmux-prof.XXXXXX)
go build -o "$WORK/bmux" ./cmd/bmux
printf 'onboarding = false\n[update]\ncheck = false\n' > "$WORK/config.toml"

# Run from an ordinary terminal, outside bmux. These paths isolate the server,
# saved sessions and config from your main server.
BMUX_RUNTIME_DIR="$WORK/r" BMUX_CONFIG="$WORK/config.toml" \
  BMUX_PPROF_DIR="$WORK/p" "$WORK/bmux" new --dir "$WORK" profiled
```

In another terminal, substitute the same `WORK` path and capture both processes
over the same 30-second window:

```sh
python3 scripts/profile-bmux.py --dir "$WORK/p" --output "$WORK/animated" --seconds 30
```

The script uses only Python's standard library, never starts or stops bmux, and
writes private files named `client-PID.cpu.pprof`, `server-PID.cpu.pprof`, and
matching `.allocs.pprof` files. It refuses to overwrite an earlier capture.
Use a new output directory for each workload. For example, repeat with
`--output "$WORK/idle"` after stopping the animation, keeping the pane size and
contents otherwise unchanged. Allocation profiles cover the capture window,
rather than cumulative allocations since startup.

Inspect a captured file with the matching test binary:

```sh
go tool pprof -top "$WORK/bmux" "$WORK/animated/client-PID.cpu.pprof"
go tool pprof -top -cum "$WORK/bmux" "$WORK/animated/server-PID.cpu.pprof"
go tool pprof -top -alloc_space "$WORK/bmux" "$WORK/animated/client-PID.allocs.pprof"
```

Replace `PID` with the number in the captured filename. CPU profiles identify
where CPU time is spent; allocation profiles show where memory was allocated,
not necessarily a leak. Treat profiles as local diagnostics: they include
function names and source paths. Check total CPU and input latency before and
after a change, not only percentages within a profile.

For manual captures, use `curl --unix-socket SOCKET` with
`http://localhost/debug/pprof/profile?seconds=30`,
`/debug/pprof/allocs?seconds=30`, or `/debug/pprof/heap`. The hostname is only an
HTTP header; the connection stays on the private Unix socket.

When finished, detach the test client with Ctrl-a d, then stop only the isolated
server using `kill "$(cat "$WORK/r/server.pid")"`. This ends its test programs;
it does not affect the main server. No temporary server should be left running
after a profiling experiment.

## Backspace moves pane focus

ASCII BS (`0x08`) and Ctrl-h are the same input. Configure your terminal's
Backspace key to send **DEL (`0x7f`)** instead. Normal Enter and DEL Backspace
reach applications. See [editor navigation](/bmux/guides/editor-navigation/) for Ctrl-h/j/k/l.

## Broken powerline separators

Run `BMUX_POWERLINE_ASCII=1 bmux` to use plain-text separators. This works even
when the terminal reports glyph support but the font renders the glyphs poorly.

## Shift+Enter inserts no newline

Programs see Shift+Enter, Ctrl+Enter, Shift+arrows and other modified keys only
if they ask for them. bmux follows the kitty keyboard protocol (`CSI > 1 u`) and
xterm's `modifyOtherKeys` (`CSI > 4 ; 2 m`) per pane. A program that enabled
either gets `CSI 13 ; 2 u` or `CSI 27 ; 2 ; 13 ~` for Shift+Enter. Any other
program gets a plain Enter, as in a legacy terminal; its modified arrows, Home,
End, Page Up/Down, Insert, Delete and function keys use the xterm `CSI 1 ; mod`
forms. Your outer terminal must also report the modifier, through the kitty
keyboard protocol or `modifyOtherKeys`. A terminal that doesn't sends a plain
Enter for Shift+Enter, and bmux can't tell it apart. If a program that supports
the protocol still can't tell the keys apart, check its own setting for the
newline key.

## Physical terminal needs padding

Run `BMUX_TERMINAL_PADDING=1 bmux`. Modern terminal emulators normally use the
default, with padding disabled.

## Terminal compatibility

The sixel and iTerm2 image protocols are not supported yet. Links open with
Ctrl-click (see [Selection and scrollback](/bmux/guides/selection-scrollback/#links)). CI runs on
macOS and Ubuntu; most day-to-day interactive use has been on macOS. For a bug
report, include your OS, terminal emulator, `bmux version`, and reproduction steps.

## Images in panes

bmux stores the images a program draws in a pane, such as a screenshot an agent
shares, and shows them again after scrolling, splitting, switching windows
or detaching and reattaching. Today that works for programs that use the
[Kitty graphics protocol](https://sw.kovidgoyal.net/kitty/graphics-protocol/)
with Unicode placeholders (`omp` does), in a terminal that supports it: Ghostty,
Kitty, Rio and WezTerm.

At startup bmux asks the terminal whether it shows Kitty graphics; set
`ui.graphics` to `"on"` to skip the question or `"off"` to never draw images.
Where images are off, or the terminal can't show them (Alacritty, macOS
Terminal.app), the image's cells are blank with `[image 800×600]` in its first
row when it fits. Two clients on one session each get what their terminal can
show.

- **Nothing shows in a supported terminal.** `NO_COLOR` and
  `TCELL_TRUECOLOR=disable` turn images off: they carry an image's id in a text
  color, which those settings stop bmux from sending exactly. Unset them for
  bmux, or accept the blank form.
- **The program picked the wrong protocol.** Panes keep the environment of the
  terminal that started the server, so a program that chooses from
  `TERM_PROGRAM` may guess wrong after you attach from another terminal.
- **Limits.** Each pane keeps up to 16 MiB of images and the server 128 MiB; the
  oldest images no cell shows go first. An image id above 16,777,215 and
  animations are not supported. Images aren't saved when bmux saves sessions,
  like scrollback: after `bmux restart` the layout is back and the images are
  not.
- **Copy, search and `bmux capture`** treat an image's cells as blanks, so
  nothing from the image reaches the clipboard.
