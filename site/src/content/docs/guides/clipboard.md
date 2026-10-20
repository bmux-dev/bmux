---
title: "Clipboard and paste"
description: "bmux copies selections on mouse release; clipboard access happens on the machine running the attached client."
sidebar:
  order: 6
---

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
