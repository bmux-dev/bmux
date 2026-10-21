---
title: "Layout files"
description: "A .bmux.toml declares the windows, splits and pane commands a session in that folder starts with."
sidebar:
  order: 3
---

A `.bmux.toml` in a folder declares the windows, splits and pane commands a
session there starts with, like tmuxinator or tmuxp. Bare `bmux`, `bmux new`,
the picker's n and the new-session popup all build it when they create a session
in that folder. It's the restore format, in TOML:

```toml
active = 1                  # the window to show first; default 1

[[windows]]
name = "edit"
focused = 1                 # default: the first pane
[windows.layout]
direction = "horizontal"    # side by side; "vertical" stacks
ratio = 6000                # the first part's share, in ten-thousandths; default 5000
[windows.layout.first]
pane = 1
[windows.layout.second]
pane = 2

[[windows]]
name = "server"
[windows.layout]
pane = 3

[[panes]]
id = 1
command = "nvim ."

[[panes]]
id = 2                      # a shell in the project folder

[[panes]]
id = 3
dir = "web"                 # relative to the folder, absolute, or ~/
command = "npm run dev"
```

A split's `first` and `second` can be splits themselves. Each command is one
line typed into its pane's fresh shell, as if you typed it. Every pane must
appear in exactly one window, directories must exist, and unknown settings
are errors: a file with a problem gives one pane, and the status line says
what's wrong.

A folder's `.bmux.toml` can come from anyone who can write to the repository,
so **its commands run only once you trust it**: `bmux layout trust` in that
folder trusts its current content. Until then, sessions get its windows and
panes and the status line says the commands were skipped. Editing the file
untrusts it again, like direnv. Trusted files are recorded by path and
SHA-256 in `trusted-layouts.json` next to the saved sessions.

`bmux layout save` writes a session's current windows to its folder's
`.bmux.toml` and trusts it. Pane directories are where each shell is now;
agents get their resume command. bmux can't see what else a pane runs, so
commands from the file being replaced are kept for panes without an agent.
