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
  the other gets a shell, so one conversation never opens twice;
- editors, pagers, `man`, monitors and `ssh` (`restore.relaunch`), by typing
  the command line they were started with into the restored shell: `nvim
  main.go`, `man tar`, `less build.log`, `ssh host`. Vim or Neovim started
  without files loads the directory's `Session.vim` (`nvim -S`) when there is
  one. Only the command the shell started counts, and only when nothing else
  shares its job: `git log | less` or `tail -f log | grep error` restore as a
  shell, since running one side of the pipe alone would show something else.

Nothing else that was running comes back: no scrollback, no servers, builds
or other programs. Resumed agents and relaunched programs are new processes
started with the same arguments, not the processes that were running: an
editor's unsaved changes and a pager's position are gone.

The default list is `vi`, `vim`, `nvim`, `emacs`, `nano`, `hx`, `man`,
`less`, `more`, `tail`, `top`, `htop`, `btop` and `ssh`. Their command lines
are saved in the sessions file, readable only by you; other programs'
arguments are never read. `restore.relaunch = []` turns relaunching off.

Until you attach to a restored session, the session picker and the sidebar
mark it `restored`, so you can see which sessions lost their running programs
without opening each one. Sessions you've attached to since, and sessions
created after the server started, aren't marked.

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
