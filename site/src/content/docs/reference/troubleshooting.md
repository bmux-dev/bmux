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

Graphics protocols are not supported yet. Links open with Ctrl-click (see
[Selection and scrollback](/bmux/guides/selection-scrollback/#links)). CI runs on
macOS and Ubuntu; most day-to-day interactive use has been on macOS. For a bug
report, include your OS, terminal emulator, `bmux version`, and reproduction steps.
