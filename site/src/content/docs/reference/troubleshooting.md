---
title: "Troubleshooting"
description: "Fixes for protocol mismatches, a server that will not start, focus keys, powerline glyphs, padding and terminal compatibility."
sidebar:
  order: 3
---

## Protocol version mismatch

An upgraded client may not work with a server started by an older binary.
Run `bmux restart` with the new binary: it saves the sessions, stops the old
server, and starts the new one, which restores them. Servers from before
saving existed are stopped with SIGTERM and their sessions are not restored.
Do not delete the socket to try to upgrade a running server.

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

## Physical terminal needs padding

Run `BMUX_TERMINAL_PADDING=1 bmux`. Modern terminal emulators normally use the
default, with padding disabled.

## Terminal compatibility

Clickable hyperlinks and the sixel and iTerm2 image protocols are not supported
yet. CI runs on macOS and Ubuntu; most day-to-day interactive use has been on macOS. For a bug
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
