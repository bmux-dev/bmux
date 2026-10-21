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

Graphics protocols are not supported yet. Links open with Ctrl-click (see
[Selection and scrollback](/bmux/guides/selection-scrollback/#links)). CI runs on
macOS and Ubuntu; most day-to-day interactive use has been on macOS. For a bug
report, include your OS, terminal emulator, `bmux version`, and reproduction steps.
