// Package protocol defines the local, versioned client/server wire format.
package protocol

import (
	"strconv"
	"time"
)

const Version = 7

// Shared PTY geometry bounds; preview panels do not resize these terminals.
const MaxTerminalWidth = 500
const MaxTerminalHeight = 200

const MaxPreviewWidth = 4096
const MaxPreviewHeight = 2048

// ViewportOrigin follows tmux's cursor-near-one-third framing. Hidden cursors
// are handled by the caller choosing the top-left origin instead.
func ViewportOrigin(cursor, sourceSize, viewportSize int) int {
	return max(0, min(cursor-viewportSize/3, max(0, sourceSize-viewportSize)))
}

const MaxPasteBytes = 4 << 20
const PasteMarkerBytes = 12
const MaxInputBytes = MaxPasteBytes // decoded payload, excluding transport/terminal markers
const PasteChunkBytes = 64 << 10
const MaxRequestBytes = 1 << 20

type Request struct {
	Size       int    `json:"size,omitempty"`
	Generation string `json:"generation,omitempty"`
	Revision   uint64 `json:"revision,omitempty"`
	Seq        int    `json:"seq,omitempty"`
	Version    int    `json:"version"`
	Op         string `json:"op"`
	Name       string `json:"name,omitempty"`
	NewName    string `json:"new_name,omitempty"`
	Dir        string `json:"dir,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Text       string `json:"text,omitempty"`
	Code       rune   `json:"code,omitempty"`
	Mod        int    `json:"mod,omitempty"`
	Delta      int    `json:"delta,omitempty"`
	X          int    `json:"x,omitempty"`
	Y          int    `json:"y,omitempty"`
	Button     int    `json:"button,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Focused    bool   `json:"focused,omitempty"`
	PaneID     int    `json:"pane_id,omitempty"`
	// NeighborID is the pane across a dragged divider from PaneID.
	NeighborID int    `json:"neighbor_id,omitempty"`
	SessionID  uint64 `json:"session_id,omitempty"`
	Direction  string `json:"direction,omitempty"`
	// Window is a 1-based window number for select-window, rename-window and
	// close-window; 0 means the active window.
	Window int `json:"window,omitempty"`
	// Args is the command an agent reports for resuming its conversation.
	Args []string `json:"args,omitempty"`
	// IfSeq makes send-input conditional: it is refused unless the pane's
	// agent seq still equals it. 0 sends unconditionally.
	IfSeq uint64 `json:"if_seq,omitempty"`
	// Input is send-input's key presses and pastes, written in order as one
	// batch.
	Input []Input `json:"input,omitempty"`
	// Range is history-text's selection.
	Range *TextRange `json:"range,omitempty"`
}

// Input is one key press or one paste for send-input. Paste, when set, is
// sent as a bracketed paste; otherwise Code and Mod are a key press in
// ultraviolet key codes and modifiers, as in the attached key operation.
type Input struct {
	Code  rune   `json:"code,omitempty"`
	Mod   int    `json:"mod,omitempty"`
	Paste string `json:"paste,omitempty"`
}

// TextRange selects text in a pane's history for history-text. Rows are
// absolute row IDs as frames number them (TerminalFrame.Base + y) within
// Epoch; a different epoch means the rows moved and the request fails.
// Mode is "char" (from From to To), "line" (whole rows) or "block" (the
// same columns of every row).
type TextRange struct {
	Epoch uint64 `json:"epoch"`
	FromX int    `json:"from_x"`
	FromY int    `json:"from_y"`
	ToX   int    `json:"to_x"`
	ToY   int    `json:"to_y"`
	Mode  string `json:"mode"`
}

type Session struct {
	Name string `json:"name"`
	// ID is the session identifier that BMUX_SESSION_ID holds; it survives
	// renames.
	ID       uint64         `json:"id,omitempty"`
	Dir      string         `json:"dir"`
	Created  time.Time      `json:"created"`
	Used     time.Time      `json:"used"`
	Clients  int            `json:"clients"`
	Exited   bool           `json:"exited"`
	Panes    int            `json:"panes"`
	Windows  int            `json:"windows,omitempty"`
	Activity []PaneActivity `json:"activity,omitempty"`
}

// Agent states reported from inside a pane with `bmux agent`.
const (
	AgentWorking = "working"
	AgentBlocked = "blocked"
	AgentIdle    = "idle"
)

// PaneActivity lists a pane only when it has a reported agent or unseen
// attention (bell, OSC 9/777 notification, blocked agent or finished turn).
type PaneActivity struct {
	Pane      int    `json:"pane"`
	Agent     string `json:"agent,omitempty"`
	State     string `json:"state,omitempty"`
	Attention bool   `json:"attention,omitempty"`
	// Seq changes whenever Agent or State does. It is unique across all panes
	// of a server, so a stale value never matches a different pane.
	Seq uint64 `json:"seq,omitempty"`
}

// Cell colors are "" for the terminal default, "#rrggbb" for an exact color, or
// a decimal palette index ("0"-"255") rendered with the attached terminal's theme.
func PaletteColor(index int) string {
	return strconv.Itoa(index)
}

type Cell struct {
	Text      string `json:"t,omitempty"`
	Width     int    `json:"w,omitempty"`
	FG        string `json:"f,omitempty"`
	BG        string `json:"b,omitempty"`
	Attrs     uint8  `json:"a,omitempty"`
	Underline bool   `json:"u,omitempty"`
	// Link is 1 + the index in TerminalFrame.Links of the OSC 8 hyperlink
	// the cell belongs to; 0 when it has none.
	Link int `json:"l,omitempty"`
}

type TerminalFrame struct {
	Epoch         uint64   `json:"epoch,omitempty"`
	HistoryStart  int      `json:"history_start,omitempty"`
	Name          string   `json:"name"`
	Title         string   `json:"title,omitempty"`
	Rows          [][]Cell `json:"rows"`
	Base          int      `json:"base"`
	Offset        int      `json:"offset"`
	History       int      `json:"history"`
	CursorX       int      `json:"cursor_x"`
	CursorY       int      `json:"cursor_y"`
	CursorVisible bool     `json:"cursor_visible"`
	CursorStyle   int      `json:"cursor_style"`
	CursorBlink   bool     `json:"cursor_blink"`
	Mouse         bool     `json:"mouse"`
	Alt           bool     `json:"alt"`
	Exited        bool     `json:"exited"`
	// Wrapped[y] reports that row y continues onto row y+1 because the
	// terminal wrapped it; copies join such rows without a newline. Nil when
	// no row is wrapped.
	Wrapped []bool `json:"wrapped,omitempty"`
	// Links holds the targets of the OSC 8 hyperlinks on screen; cells refer
	// to them by Cell.Link, so a long URL is sent once per frame.
	Links []string `json:"links,omitempty"`
}

type PaneFrame struct {
	ID       int            `json:"id"`
	X        int            `json:"x"`
	Y        int            `json:"y"`
	Width    int            `json:"width"`
	Height   int            `json:"height"`
	Terminal *TerminalFrame `json:"terminal"`
}

type Frame struct {
	Generation  string `json:"generation,omitempty"`
	PreviewMode string `json:"preview_mode,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	Name        string `json:"name"`
	Focused     int    `json:"focused"`
	Zoomed      bool   `json:"zoomed"`
	// Edge counts directional focus moves that found no neighbor pane;
	// EdgeDirection is the latest one. Clients compare counts, never act on
	// the first frame of an attachment.
	Edge          uint64 `json:"edge,omitempty"`
	EdgeDirection string `json:"edge_direction,omitempty"`
	// Config counts server config reloads; clients reread their own
	// settings when it changes.
	Config uint64 `json:"config,omitempty"`
	// Clipboard is text an app in this session copied with OSC 52 since the
	// previous frame of this attachment; the client copies it like a selection.
	Clipboard string `json:"clipboard,omitempty"`
	// Notice is something the client should show once, such as a layout
	// file that couldn't be used; only the session's first attachment gets it.
	Notice string `json:"notice,omitempty"`
	// Update is a newer bmux release for the status bar's hint; nil when the
	// server runs the newest one, or update checks are off.
	Update *UpdateHint `json:"update,omitempty"`
	Panes  []PaneFrame `json:"panes"`
	// Windows lists the session's windows in order; the panes above are the
	// active one's.
	Windows []Window `json:"windows,omitempty"`
}

// UpdateHint names a newer release: available to install with bmux update,
// or Installed already, waiting for bmux restart to switch the server.
type UpdateHint struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed,omitempty"`
}

// Window describes one window of a session. Name is "" until it's renamed;
// Title is then its focused pane's title, for clients to show instead.
type Window struct {
	Name   string `json:"name,omitempty"`
	Title  string `json:"title,omitempty"`
	Active bool   `json:"active,omitempty"`
	Panes  []int  `json:"panes"`
}

type Response struct {
	Revision    uint64    `json:"revision,omitempty"`
	NotModified bool      `json:"not_modified,omitempty"`
	Ack         int       `json:"ack,omitempty"`
	Version     int       `json:"version"`
	Error       string    `json:"error,omitempty"`
	Sessions    []Session `json:"sessions,omitempty"`
	Frame       *Frame    `json:"frame,omitempty"`
	// Command names the program in front of a pane's shell (pane-command);
	// "" when the shell itself is in front.
	Command string `json:"command,omitempty"`
	// Pane is the target pane's activity after capture-pane or send-input,
	// including a refused send; Text is capture-pane's visible screen, or the
	// layout op's layout file for directory Dir.
	Pane *PaneActivity `json:"pane,omitempty"`
	Text string        `json:"text,omitempty"`
	Dir  string        `json:"dir,omitempty"`
	// Env is the server's own environment (environment), which a restart
	// started from inside a pane gives the new server.
	Env []string `json:"env,omitempty"`
	// Range is search-history's match: From is its first cell, To its last.
	Range *TextRange `json:"range,omitempty"`
	// Zoomed is zoomed's answer: whether the window showing the pane is zoomed.
	Zoomed bool `json:"zoomed,omitempty"`
}
