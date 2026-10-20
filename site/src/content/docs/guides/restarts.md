---
title: "Restarts and reboots"
description: "The server saves every session every few seconds and restores layouts after an upgrade, a crash or a reboot."
sidebar:
  order: 7
---

The server saves every session every few seconds and when it stops cleanly
(`bmux restart`, SIGTERM or SIGINT). The next server, whether after an upgrade,
a crash or a reboot, restores them before accepting clients:

- the same sessions and names, windows and their names, the active window,
  and each window's split layout, focused pane and zoom;
- a fresh login shell in each pane, in the directory that pane's shell was in;
- agents, by typing a resume command into the restored shell. Reported
  commands run as given. Otherwise recognized agents continue their latest
  conversation in that directory: `claude --continue`, `codex resume --last`,
  `opencode --continue`, `omp --continue`, `pi --continue`. When two panes
  would continue the same agent in the same directory, only the first does;
  the other gets a shell, so one conversation never opens twice.

Nothing else that was running comes back: no scrollback, no editors, servers
or other programs. Resumed agents are new processes reopening their saved
conversation, not the processes that were running.

`bmux restart` from inside a pane finishes in the background, because stopping
the server ends that pane's jobs. Attached clients reconnect to the session
they showed once the new server is up. Run `bmux attach`, not bare `bmux`, to
return to a restored session from a new terminal: bare `bmux` creates another.

New panes inherit the server's environment. A restart from inside a pane
keeps the running server's environment, so variables that the pane's shell or
its programs added (an agent's markers, an activated virtualenv) never reach
every pane. A restart from a terminal outside bmux uses that terminal's
environment instead: that's how to change what panes start with.

Sessions are saved to `~/Library/Application Support/bmux/sessions.json` on
macOS and `${XDG_STATE_HOME:-~/.local/state}/bmux/sessions.json` on Linux, or
`sessions.json` inside a custom `BMUX_RUNTIME_DIR`, so separate servers keep
separate sessions. A file bmux can't read is moved to `sessions.json.unreadable`
and the server starts without restoring.
