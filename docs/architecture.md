# Ownership and contracts

## Responsibilities

- `cmd/bmux`: CLI argument handling and lifecycle entry points.
- `internal/daemon`: local server discovery/startup; only depends on protocol RPC.
- `internal/protocol`: wire framing, shared payload/name/identity contracts, one-shot RPC.
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

Production screens deliver one `rawPasteEvent` after capturing bracketed bytes
before key interpretation. There is no second key-reconstruction paste path.
`handleRawPaste` handles session/prompt/filter intent and visible rejection.

Bare Ctrl-h/j/k/l use `navigate-pane`, preserving the existing sequence/ack fence.
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
