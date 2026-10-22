# bmux for agents

bmux is a terminal multiplexer: persistent sessions, each split into panes,
each pane a shell or a program such as a coding agent. These commands let you,
an agent, see the other panes, read them, type into them and wait for them,
for any program. bmux never reads meaning into screen text; you do.

## Am I inside bmux?

Inside a pane, bmux sets:

- `BMUX_SESSION`: the session name when the pane started
- `BMUX_SESSION_ID`: the session's identifier, unchanged by renames
- `BMUX_PANE`: the pane number

`bmux capabilities` prints the bmux version, protocol version, every command
and, under `pane`, where you are (`null` outside bmux).

## Targets

Commands take a target, written like tmux's `session:window.pane`:

| Target | Means |
| --- | --- |
| `api` | session `api`: the focused pane of its active window |
| `api:7` | pane 7. Pane numbers are unique across a session's windows, so this reaches a pane in a background window too; it's the form `bmux agents` prints |
| `api:2.` | window 2: its focused pane |
| `api:2.1` | window 2, the first pane in its layout |
| `:2.1`, `:7`, `:` | the same, in the session you're running in |

`api:N` always means pane N, not window N as in tmux; when no pane N exists
but window N does, the error says to write `api:N.`.
`bmux agents` lists every pane running a coding agent:

```
$ bmux agents
TARGET  AGENT   STATE    SEQ
api:1   omp     blocked  57   needs you
web:2   claude  working  61
ops:1   omp     idle     12   (this pane)
```

`--json` prints the same as a list of objects with `target`, `session`,
`session_id`, `pane`, `window`, `index` (the pane's window and position in
it), `agent`, `state`, `seq`, `attention` and `self`.

States: `working` (busy), `blocked` (waiting on a decision: a question, a
permission prompt), `idle` (turn finished, waiting for a prompt). Agents with
a bmux integration report them exactly; others are recognized by process name
and read from output timing, which shows blocked as idle.

**Seq** changes every time a pane's agent or state does, and is never reused,
even across panes. Pass it back to `bmux send --if-seq` so you only type into
the state you looked at.

## Read a pane

```
bmux capture api:1          # visible text
bmux capture --json api:1   # {"target","window","index","agent","state","seq","text"}
```

This is the screen as the user would see it: the question, its options and
which option is highlighted. Read it before you answer anything.

## Type into a pane

```
bmux send api:1 --if-seq 57 --key down --key enter
bmux send api:1 --if-seq 57 --text "use Postgres" --key enter
```

`--key` takes the key names the bmux config uses: `enter`, `esc`, `tab`,
`backspace`, `up`, `down`, `left`, `right`, `home`, `end`, `pageup`,
`pagedown`, `space`, `F1`–`F12`, a character such as `y`, and modifiers `C-`
(Ctrl), `A-` (Alt), `S-` (Shift): `C-c`, `A-x`. `--text` pastes text as the
user's terminal would. Keys and text are sent in the order given, as one
batch, without resizing the session, moving focus or marking it seen.

`send` needs a target that names a pane: `SESSION:PANE` or
`SESSION:WINDOW.N`, never just a session or window. With `--if-seq`, nothing is typed unless the
pane's seq still matches; otherwise it fails with `stale`. Capture again and
look at what changed instead of retrying the same keys.

## Change the layout

```
bmux split -h -c ~/code/api -- "npm run dev"   # prints the new pane: api:4
bmux split api:4 -v -d -- tail -f log         # -d keeps focus where it was
bmux new-window -n tests -- go test ./...
bmux select api:2.            # a window; bmux select api:4 focuses a pane
bmux resize api:4 left 10     # move its divider 10 cells; 5 by default
bmux zoom api:4
bmux rename-window api:2. tests
bmux kill-pane api:4          # and kill-window: no confirmation
```

Without a TARGET they act on the pane you run them in. `split` and
`new-window` start in the target pane's directory unless `-c DIR` says
otherwise, type the command after `--` into the new shell (one argument is a
command line, several are quoted as arguments), and print the new pane's
target. `kill-pane` and `kill-window` always need a TARGET. Ask the user
before closing a pane you didn't open.

## Wait for a pane

```
bmux wait-agent --since 57
```

Exits as soon as an agent pane other than yours is `blocked` or `idle` with a
seq above `--since`, printing it as one `bmux agents --json` object. With
several, it prints the lowest seq, so waiting again with `--since` set to the
printed seq walks through every change in order; a wait still pending when
you start is reported at once. `--state` narrows the states, targets narrow
the panes (`bmux wait-agent --state blocked api`), and `--timeout 10m` gives
up with an error.

It blocks until something happens, and so does anything that runs it,
including your own background jobs: never run it yourself while the user
talks to you. To be told about other agents, use `--notify` (next section).

## Get told when other agents need the user

```
bmux wait-agent --notify api:2
```

runs until stopped, in a pane of its own. Whenever another agent pane becomes
`blocked` or `idle` (not `api:2` itself), it waits until the agent in `api:2`
is idle and types one line into it, as if the user had:

```
[bmux] web:1 (claude) is blocked and waiting on the user, seq 57. Read it with bmux capture --json web:1; answer with bmux send web:1 --if-seq 57.
```

It types one change at a time, keeps only each pane's newest change, drops
panes that went back to work, and types nothing more until the receiving
agent has handled the last line. At start it reports only panes already
blocked. You never run a waiting command, so you stay free to talk.

## Supervise other agents

1. `bmux agents --json`: tell the user about panes already blocked.
2. Unless a notifier for your pane already runs, start one in a new session
   of its own; `$BMUX_SESSION:$BMUX_PANE` (or `pane.target` in
   `bmux capabilities`) is your pane:
   ```
   bmux new --detach "notify-$BMUX_SESSION"
   bmux send "notify-$BMUX_SESSION:1" --text "bmux wait-agent --notify $BMUX_SESSION:$BMUX_PANE" --key enter
   ```
   `bmux kill "notify-$BMUX_SESSION"` stops it.
3. When a `[bmux]` line arrives, `bmux capture --json TARGET` and tell the
   user in one sentence which agent it is and what it asks, with the options.
4. When the user answers, work out the keys from the capture (where the
   highlight is, how many `down`s, `enter`) and
   `bmux send TARGET --if-seq SEQ --key ...`. On `stale`, capture again and
   say what changed.
5. Asked what an agent is doing, capture it and summarize.

## Rules

- Answer an agent's question only with the user's answer. Never pick for them.
- Read a permission prompt's command back to the user and get a yes before
  approving anything destructive.
- Always pass `--if-seq` when typing into an agent's pane.
- Never type into a shell pane, or run `bmux kill`, without the user asking,
  except the notifier session you started for yourself.
- Report your own state, if you are an agent without an integration:
  `bmux agent working`, `bmux agent blocked`, `bmux agent idle`.
