# Ownership and contracts

## Responsibilities

- `cmd/bmux`: CLI argument handling and lifecycle entry points.
- `internal/daemon`: local server discovery/startup; only depends on protocol RPC.
- `internal/protocol`: wire framing, shared payload/name/identity contracts, one-shot RPC.
- `internal/config`: the optional config file, defaults, environment overrides and validation.
- `internal/integration`: agent hook/plugin files, their versions, and mapping hook events to `bmux agent` reports.
- `internal/server`: session layout, emulator state, owned OS jobs and cancellation.
- `internal/client`: input, modeless selection, clipboard intent and preview display.

## Lock boundaries

`Server.mu` owns the registry and admissions. It is released before session/pane
state or process work. `Server.operations` serializes registry mutations but isn't
needed by shutdown, ping, or read-only control operations.

State order is `session.mu` then `pane.mu`. The independent session shutdown
registry retains pane cleanup ownership before layout publication and is accessible
without acquiring those state locks. The PTY writer owns neither state lock.

Three milestones remain distinct:

1. The login shell exits (`pane.done`).
2. Ordinary jobs in its owned OS session are stopped (`pane.stopDone`).
3. Watchers remove its layout entry; removing the last pane closes `session.done`.

Do not collapse the two pane maps or completion channels merely to reduce fields.
Process discovery/signalling is best effort with SID and birth checks. Deliberately
detached jobs are out of scope, and POSIX has no portable atomic birth-checked kill.

A split samples the source pane under `session.mu`, releases it to read the
directory of the PTY's foreground group, then the shell's, from the OS
(`pane.currentDir`), and reacquires it to split that sampled pane. A missing
or removed directory falls back to the session's.

## Input admission

`protocol.MaxPasteBytes` is the decoded logical payload maximum: 4 MiB.
`protocol.PasteMarkerBytes` is the maximum terminal bracket overhead: 12 bytes.
The bridge's pending-user budget is their sum, separate from the bounded reply
budget. Pending user bytes include reservation, queued bytes and writer in-flight
bytes. Whole intent is admitted or rejected; accepted user bytes aren't truncated.

The emulator pipe has a fast reader. Independent FIFO writing can wait on the PTY
without blocking emulator state or shutdown. Zero-byte pipe fences synchronize
user/reply classification; they are part of the contract, not redundant writes.

## Snapshots and selection

`pane.anchoredFrame` computes cumulative-history adjustment, clamps the viewport
and copies visible cells under one state acquisition. Per-client `lastTotals`
records the returned observation, never a separately read earlier total.

Absolute row IDs plus screen epoch/history floor distinguish retained rows from
repainted/reset/evicted data. Selection cache and highlight are invalidated together
when selected data changes; stable retained scrollback isn't substituted silently.

## Input and previews

Picker deletion is a centered modal overlay over a dimmed list/cached terminal,
with immutable session identity and explicit lowercase-y confirmation. The kill
RPC runs outside the UI loop; a result channel delivers completion/errors without
freezing input or redraws. One deletion can be pending per client. Completion
does not close attachments by name, since that could affect a replacement session;
the attached-session stream owns its own exit transition. Errors do not displace
an unrelated prompt or pull the user out of an attached app. The server's owned-job
shutdown grace/escalation policy and CLI completion semantics remain unchanged.

Both views reserve row zero for the powerline and the final row for status.
Attached terminal dimensions exclude both rows; rendering, cursor coordinates,
pane dividers and mouse translation share `paneOriginY`. Chrome rejects app mouse
presses/motion/wheel events even while a stale pre-resize frame is visible, but
releases there still end selections and release application mouse buttons.

A press on a divider cell starts a `dividerDrag` holding the panes on either
side; until release, every event belongs to it, including over the sidebar and
status line. Motion to a new column/row sends `drag-divider` with that pane
pair. The server moves the split where the two panes' subtrees meet (their
lowest common ancestor), so the request names the divider even as the layout
changes under the drag, and stale pairs fail instead of moving another one.

Production screens deliver one `rawPasteEvent` after capturing bracketed bytes
before key interpretation. There is no second key-reconstruction paste path.
`handleRawPaste` handles session/prompt/filter intent and visible rejection.

Keys bound to `navigate-*` (bare Ctrl-h/j/k/l by default) use `navigate-pane`,
preserving the existing sequence/ack fence. "Editor" below means Vim/Neovim or
a program named in `keys.pass-through` (`navigationMatch`).
The server samples the focused pane under `session.mu`, releases it to query the
PTY foreground group and editor executable, then reacquires it to validate the
same focus/pane before routing. OS inspection never runs per frame or under state
locks. Neovim/Vim receive the canonical Ctrl key through the bounded input bridge;
other foreground apps use the shared directional focus logic. Inspection errors
preserve application input rather than stealing focus. Explicit prefix arrows
and clicks still use `select-pane` and bypass editor routing. No title-based
heuristic or editor configuration mutation is involved.

The live terminal view below the picker is input-read-only. It neither attaches nor
injects input or changes MRU ordering. Native cells use source glyphs/colors and
the outer terminal's font. Cursor rendering follows source visibility/shape;
no invented blink clock, bitmap renderer, or terminal image protocol remains.

`watch-viewport` copies a panel-sized presentation layout from existing source
terminal grids. Under `session.mu`, `previewSnapshot` samples shared revisions,
places the layout tree into the requested panel, and acquires each pane lock to
copy a bounded viewport around its source cursor. `protocol.ViewportOrigin`
uses tmux's cursor-near-one-third framing; a hidden cursor uses the top-left.
Cursor coordinates are translated into the copied rows. Only source cells are
copied, even if a requested presentation rectangle exceeds the native terminal.
No PTY resize, sizing lease, restoration baseline, reflow, or settling timer exists.
Generation guards bind requests to the selected instance; preview connections
accept no app input and do not count as attachments or alter recency/focus.

The first full viewport is sent immediately, without waiting for a refresh tick.
Later snapshots stream at up to 10 Hz, with unchanged-frame heartbeats. Viewers
have independent presentation dimensions and revision tracking. The client
cancels on selection/generation/geometry changes, prompts, hidden panels, or exit;
stale readers cannot replace the current preview. A new operation name prevents
older PTY-resizing servers from mutating state for a native-viewport request.
Picker titles contain only identity and list position; normal refresh/scope
metadata is not rendered as chrome.

## Sidebar and pane activity

Pane activity is never inferred from screen text or titles. Each pane holds a
reported agent label and state (`bmux agent`, one-shot RPC keyed by
`BMUX_SESSION_ID`/`BMUX_PANE`, the same trust model as `bmux focus`), a
detected foreground agent, and an attention flag. BEL, OSC 9 (except 9;4
progress) and OSC 777 `notify` set attention from emulator callbacks under
`pane.mu`; so do `blocked` reports and reported `working` -> `idle`
transitions. `working` clears it. Reports never change recent-use order.

Each session runs one agent sampler (`watchAgents`, 1 Hz, exits with the
session). It copies the pane list under `session.mu`, releases it, queries
each PTY's foreground group through `foregroundGroupCommand` (shared with
editor navigation) with no state lock held, then applies the sample under
`pane.mu`. Candidates are comm, the executable path and, only for node, bun,
deno and python, argv[1]; no other argument or environment data is read.
Query errors keep the previous sample.

Without a report, state comes from output timing. `apply` marks output unless
it arrives within `inputEchoWindow` of any pane input (keys, paste, mouse,
focus, resize). Two consecutive samples with output make a detected agent
working; two without return it to idle and, if unseen, set attention. Reports
override timing. A detected agent leaving the foreground drops its reports.

`session.viewers` counts attachments whose last `focus` request was true; the
attach loop owns each connection's contribution and releases it on exit, and
never forwards a client's `focus` request to a pane. `updateViewedLocked` runs
under `session.mu` after every layout/focus change and viewer change, stores
`pane.viewed` (atomic, read by callbacks) and clears the viewed pane's
attention. A viewed pane never gains attention. Viewed is also application
focus: each change sends the pane a focus in/out, which the emulator encodes
only while the app has `?1004` set, and the `EnableMode` callback reports focus
in when an app turns `?1004` on in a viewed pane. `list` returns
`Session.Activity` in unzoomed layout order, only for panes with an agent or
attention.

A directional move without a neighbor (`select-pane` with a direction,
`navigate-pane`, `focus-pane`) increments `session.edges` and records the
direction; snapshots carry both. A client records the count from its first
frame per attachment and moves focus into the sidebar only when the count later
changes with direction `left`. Editor-forwarded keys are not edges.

The client offsets pane layout by `sidebarWidth` columns when shown (screen
width at least `sidebarMinScreenWidth`, not hidden with Ctrl-a b). Attach,
create and resize requests send the remaining width, so the server layout
never includes sidebar columns. `drawPane`, dividers, cursor and mouse
translation all apply the same `paneOriginX`. Sidebar clicks are chrome; drags
and app presses that began in a pane still deliver motion and release there.
The sidebar polls `list` asynchronously at 1 Hz with at most one request in
flight; the picker's synchronous refresh owns the list while open.

## Agent integrations

`internal/integration` owns agents' config files; nothing else writes them.
Integrations only add ways to call `bmux agent`; the server has no idea they
exist. Claude Code and Codex get one managed script plus entries in their JSON
hook tables; the script hands the event on stdin to `bmux integration hook
AGENT`, where `HookReport` maps events to states in Go, so the script stays
trivial and needs no jq. The hook path never prints, never starts a server and
always exits 0.

omp and pi get one managed extension file each, generated from one template:
it keeps working/blocked/idle from their extension events (pi's
`agent_settled`, omp's `agent_end` without `willContinue`, prompts, approvals
and the ask tool) and runs `bmux agent` itself, serialized and only when the
report changes. Leaving the foreground already drops a recognized agent's
reports, so extensions never report `clear`. omp refuses to install into a
directory pi also loads, since each would run the other's extension.
opencode gets a server plugin in the same style: the opencode process in a
pane serves only that pane's TUI, so the plugin counts busy sessions
(subagents included) and pending permission/question requests, and resumes
the latest top-level session.

JSON edits go through an ordered object type, so other keys keep their order
and content; they remove every handler whose command names
`bmux-agent-state.sh`, drop groups and events left empty, then add one group
per event. Install is therefore idempotent and uninstall restores a file bmux
didn't otherwise touch. Every managed file carries `bmux-integration: NAME vN`;
status compares that with the agent's current version, and a script whose
hook entries are gone counts as not installed. Files are written atomically
and keep their mode.

The client's integrations panel (Ctrl-a i) calls the same package. The client
and server share a machine (the socket is local), so the files it writes are
the ones the pane's agents load. Status reads and installs are a few small
local file operations, so they run inline on the UI goroutine. Like the key
list, the panel is a modal `drawModal` overlay that takes every key and click.

## Persistence

`EnablePersistence` restores the state file before `Serve` accepts clients and
starts the saver; tests that use `New` alone never touch disk. `saveState`
(serialized by `Server.persistMu`) captures layout under `session.mu`, releases
it, reads each shell's cwd from the OS (`processCwd`), then agent label and
resume command under `pane.mu`. It writes atomically (temp file, fsync,
rename) only when the JSON changed. `Close` saves first and marks the saver
final, so pane shutdown can never overwrite what will be restored.

Restore creates new sessions (new IDs and creation times, saved recency) with
pane IDs and the layout tree kept, validating the tree; an invalid tree falls
back to one pane. Resume commands are typed into the new shell with the `type`
pane operation, which bypasses bracketed paste so the shell runs it. Built-in
"continue latest" commands are deduplicated per directory; reported ones are
exact. Only layouts, directories and agent conversations return.

`shutdown` is accepted before the protocol version check so any newer binary
can stop an older server. `daemon.Stop` waits for `server.lock`, not just the
socket, because the socket closes before panes finish stopping.

## Configuration

`config.Load` always returns a usable config: a missing file means defaults,
and each invalid or unknown setting is reported while keeping its default.
`BMUX_*` variables override the file. The client applies its part (prefix,
keys, theme, sidebar, sound) in `applySettings`; theme styles are package
variables rebuilt by `applyTheme` on the UI goroutine only. Key bindings are
two tables of canonical key names (`config.KeyName`, matched by the client's
`keyName` for tcell events) to action names: root keys are checked before the
prefix, prefix keys after it, and status-line help is generated from them.
The server stores its part
(agent names, timing, detection, restore, resume overrides) in an atomic
process-wide `serverSettings`, read on every sample and save. Terminal padding
is read only when a client creates its screen.

`reload-config` rereads the server's file and increments `configRevision`,
which every snapshot carries as `Frame.Config`; bumping each session's revision
forces a frame. Clients record the value from an attachment's first frame and
reread their own file when it later changes, so `bmux reload` reaches every
attached client. Ctrl-a r reloads the client directly and then asks the server;
SIGUSR1 to the server is the same as `bmux reload`, as in Helix. Keys follow
Helix's config layout: kebab-case names and a top-level `theme`.

## User-visible behavior details

- Attached-client snapshots are coalesced to at most 30 updates per second.
  Each snapshot carries the visible viewport for each displayed pane, not its
  full scrollback. Preview timing is described above.
- Large pastes travel as bounded chunks. The server assembles and validates the
  complete paste before submitting one logical request to the PTY input bridge.
  Incomplete assemblies never submit partial input.
- Pane shutdown has a 1.25-second budget. It sends HUP and CONT, closes PTY I/O,
  then escalates to TERM at about 0.5 seconds and KILL at about 1 second if jobs
  remain. Session/server shutdown shares a deadline across panes rather than
  adding a budget per pane.
- Picker rename and delete requests carry the selected session's creation-time
  generation. The server rejects an action if that name now identifies another
  session. Explicit CLI rename/kill commands instead target the current name.
- The client owns the outer window title. `draw` computes it from the session
  name and the focused pane's `TerminalFrame.Title` and calls `SetTitle` only
  when it changes. App titles lose control characters and are capped, so an
  app can never send escape sequences to the outer terminal through its title;
  tcell saves the outer title on start and restores it on exit.
- App clipboard writes (OSC 52) never touch a clipboard on the server. The
  emulator handler keeps the latest valid write per pane under `pane.mu`,
  numbered by the process-wide `clipboardWrites`. Each attachment starts at the
  counter's value when it attached and puts the newest later write from any
  pane, shown or hidden, into its next frame (`Frame.Clipboard`), so each copy
  reaches each client once. The client copies it before its stale-ack frame
  check, through the same worker as selections with sequence 0, which never
  replaces a selection's notice. Reads are never answered; data that fills
  x/vt's 4 MiB OSC buffer may be truncated and is dropped.
