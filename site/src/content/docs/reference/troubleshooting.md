---
title: "Troubleshooting"
description: "Fixes for protocol mismatches, a server that will not start, focus keys, powerline glyphs, padding and terminal compatibility."
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
