// Package protocol defines the local, versioned client/server wire format.
package protocol

import (
	"strconv"
	"time"
)

const Version = 3

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
	SessionID  uint64 `json:"session_id,omitempty"`
	Direction  string `json:"direction,omitempty"`
}

type Session struct {
	Name     string         `json:"name"`
	Dir      string         `json:"dir"`
	Created  time.Time      `json:"created"`
	Used     time.Time      `json:"used"`
	Clients  int            `json:"clients"`
	Exited   bool           `json:"exited"`
	Panes    int            `json:"panes"`
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
	Edge          uint64      `json:"edge,omitempty"`
	EdgeDirection string      `json:"edge_direction,omitempty"`
	Panes         []PaneFrame `json:"panes"`
}

type Response struct {
	Revision    uint64    `json:"revision,omitempty"`
	NotModified bool      `json:"not_modified,omitempty"`
	Ack         int       `json:"ack,omitempty"`
	Version     int       `json:"version"`
	Error       string    `json:"error,omitempty"`
	Sessions    []Session `json:"sessions,omitempty"`
	Frame       *Frame    `json:"frame,omitempty"`
}
