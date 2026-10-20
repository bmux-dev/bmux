---
title: "Scripting panes and agents"
description: "Scripts and coding agents can list agent panes, read them, type into them and wait for them."
sidebar:
  order: 4
---

Scripts and coding agents can list agent panes, read them, type into them and
wait for them, for any program:

```sh
bmux agents                                   # TARGET  AGENT  STATE  SEQ
bmux capture api:1                            # the pane's visible text
bmux send api:1 --if-seq 57 --key down --key enter
bmux send api:1 --text "use Postgres" --key enter
bmux wait-agent --since 57                    # blocks; prints the next pane that needs you
```

A target is `SESSION` (its focused pane) or `SESSION:PANE`; `send` requires
the pane, so input never lands in whatever happens to be focused. Keys use the
config file's names (`enter`, `esc`, `down`, `C-c`, `A-x`, `F5`); `--text`
pastes. Input is sent in order as one batch, without attaching: the session
isn't resized, focus doesn't move and the pane isn't marked seen.

Each agent pane has a **seq** that changes whenever its agent or state does
and is never reused, even across panes. `bmux send --if-seq` types nothing,
and fails with `stale`, unless the seq still matches, so an answer meant for
one question never reaches the next. `bmux wait-agent` skips the pane it runs
in and prints the lowest seq above `--since`, so waiting again with each
printed seq walks through every change in order. `--json` on `agents` and
`capture` includes the seq.

`bmux wait-agent --notify SESSION:PANE` keeps running and, instead of
printing, types one `[bmux] … is blocked …` line into that agent's pane for
each change, only while that agent is idle and only after it has handled the
previous line. It keeps just each pane's newest change and drops panes that
went back to work. Run it in a pane of its own: a supervising agent that ran a
waiting command itself, even as a background job, would stay busy and stop
answering its user.

`bmux guide` prints the same for coding agents, with the supervision loop and
rules, and `bmux --help` points agents to it. The Claude Code, omp and pi
integrations also install a `bmux` skill, so those agents know about it
without exploring.
