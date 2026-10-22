---
title: "Compatibility"
description: "What an upgrade keeps working: saved sessions, config files and commands from every earlier release."
sidebar:
  order: 4
---

An upgrade keeps what you already have working. The tests check this against
real output from every release since v0.1.0, kept in `testdata/releases/`:

| What | Promise | Checked against |
| --- | --- | --- |
| Saved sessions | A save file written by any release since v0.2.0 restores: the same sessions, windows, splits, focus and pane directories | The `sessions.json` each release's server wrote |
| Config files | Every setting any release documented still loads with the same meaning | Each release's `bmux default-config`, with every setting turned on |
| Commands | Every command and flag any release listed in `bmux --help` still works | Each release's `bmux --help` |

## What can change

- **The protocol** between the client and the server changes between
  releases. After upgrading, commands say the server is an older version until
  you run `bmux restart`, which saves your sessions, starts the new server and
  restores them. Programs running in panes end. The release notes say when
  that's needed.
- **Removed settings and key actions** keep loading: the status line (or
  `bmux reload`) names them once, and everything else in the file applies.
  They're listed below with what replaced them.
- **Output meant for people** (status line messages, help text, `bmux list`
  columns) can change. Scripts should use `bmux list-panes -F`,
  `list-windows -F`, `display-message -p` or `--json`.

| Removed | In | Instead |
| --- | --- | --- |
| `projects.zoxide` | v0.6.0 | The new-session popup lists your sessions' folders and `projects.roots` |
| The `projects` key action (Ctrl-a f) | v0.5.2 | `new-session` (Ctrl-a n), whose Projects tab is the old picker |

bmux is still 0.x, so this can't yet cover everything. 1.0 freezes these
formats; until then, any break beyond the lists above is a bug.
