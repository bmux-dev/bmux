---
title: "Reporting agent state"
description: "Agents without an integration, or your own scripts, can report exact state by calling bmux agent from inside their pane."
sidebar:
  order: 3
---

Agents without an integration, or your own scripts, can report exact state by
calling `bmux agent` from inside their pane; `BMUX_SESSION_ID` and `BMUX_PANE`
identify the pane. Reports replace output timing for that pane. Report
`working` when a turn starts, `idle` when it ends, `blocked` when the agent
needs a decision, and `clear` when it exits. The optional name (at most 40
bytes) labels the sidebar row and persists across reports. A report never
starts a server and fails outside a bmux pane. Reports from a recognized agent
are dropped when it leaves the foreground. Agents bmux doesn't recognize can
still report themselves.

After `--`, a report can include the command that reopens this conversation,
such as `bmux agent idle claude -- claude --resume "$SESSION_ID"`. It replaces
the built-in resume command when the server restarts. The first word must be
a command name on `PATH`; at most 64 arguments and 8 KiB, no control
characters.

Agents without hooks also mark their pane when they ring the bell or send a
terminal notification. An unrecognized agent that exits without reporting
`clear` keeps its last state until something in that pane reports again.
