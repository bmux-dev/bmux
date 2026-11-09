package server

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// These wire types freeze the protocol-10 legacy decoder's cell/terminal shape:
// they must not acquire compact_rows or depend on the new TerminalFrame type.
type legacyCell struct {
	Text      string `json:"t,omitempty"`
	Width     int    `json:"w,omitempty"`
	FG        string `json:"f,omitempty"`
	BG        string `json:"b,omitempty"`
	Attrs     uint8  `json:"a,omitempty"`
	Underline bool   `json:"u,omitempty"`
	Link      int    `json:"l,omitempty"`
}
type legacyTerminal struct {
	Epoch         uint64           `json:"epoch,omitempty"`
	HistoryStart  int              `json:"history_start,omitempty"`
	Name          string           `json:"name"`
	Title         string           `json:"title,omitempty"`
	Rows          [][]legacyCell   `json:"rows"`
	Base          int              `json:"base"`
	Offset        int              `json:"offset"`
	History       int              `json:"history"`
	CursorX       int              `json:"cursor_x"`
	CursorY       int              `json:"cursor_y"`
	CursorVisible bool             `json:"cursor_visible"`
	CursorStyle   int              `json:"cursor_style"`
	CursorBlink   bool             `json:"cursor_blink"`
	Mouse         bool             `json:"mouse"`
	Alt           bool             `json:"alt"`
	Exited        bool             `json:"exited"`
	Wrapped       []bool           `json:"wrapped,omitempty"`
	Links         []string         `json:"links,omitempty"`
	Images        []protocol.Image `json:"images,omitempty"`
}

func openCompactAttach(t *testing.T, server *Server, encoding string) (net.Conn, *json.Decoder, <-chan struct{}) {
	t.Helper()
	c, peer := net.Pipe()
	done := make(chan struct{})
	go func() { defer close(done); defer peer.Close(); server.handle(peer) }()
	t.Cleanup(func() {
		c.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("attach handler leaked")
		}
	})
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := protocol.EncodeRequest(json.NewEncoder(c), protocol.Request{Version: 10, Op: "attach", Name: "view", Width: 100, Height: 30, FrameEncoding: encoding}); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(c)
	var ack protocol.Response
	if err := decoder.Decode(&ack); err != nil {
		t.Fatal(err)
	}
	selected := ""
	if encoding == protocol.CellPaletteRunsV1 {
		selected = encoding
	}
	if ack.Version != 10 || ack.Error != "" || ack.FrameEncoding != selected {
		t.Fatalf("attach acknowledgement: %#v", ack)
	}
	return c, decoder, done
}

func compactAttachFrame(t *testing.T, decoder *json.Decoder, encoding string, predicate func(*protocol.Frame) bool) *protocol.Frame {
	t.Helper()
	for {
		var response protocol.Response
		if err := protocol.DecodeResponse(decoder, encoding, &response); err != nil {
			t.Fatal(err)
		}
		if response.Error != "" {
			t.Fatal(response.Error)
		}
		if response.Frame != nil && predicate(response.Frame) {
			return response.Frame
		}
	}
}

func TestCompactServerLegacyAndUnknownEncoding(t *testing.T) {
	for _, encoding := range []string{"", "future-encoding"} {
		t.Run(encoding, func(t *testing.T) {
			server, _ := viewportSession(t)
			_, decoder, _ := openCompactAttach(t, server, encoding)
			// Decode the actual response with the frozen legacy terminal shape and
			// separately inspect raw JSON: unknown-field tolerance must not hide a
			// compact-only frame from the old decoder.
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "compact_rows") || strings.Contains(string(raw), "frame_encoding") {
				t.Fatal("compact fields sent without negotiation")
			}
			var legacy struct {
				Version int `json:"version"`
				Frame   *struct {
					Panes []struct {
						Terminal *legacyTerminal `json:"terminal"`
					} `json:"panes"`
				} `json:"frame"`
			}
			if err := json.Unmarshal(raw, &legacy); err != nil {
				t.Fatal(err)
			}
			if legacy.Version != 10 || legacy.Frame == nil || len(legacy.Frame.Panes) != 1 || len(legacy.Frame.Panes[0].Terminal.Rows) != 30 || len(legacy.Frame.Panes[0].Terminal.Rows[0]) != 100 {
				t.Fatal("legacy client cannot render frame")
			}
		})
	}
}

func TestCompactAttachResizeLayoutMetadataAndReattach(t *testing.T) {
	server, session := viewportSession(t)
	session.mu.Lock()
	session.notice = "first-attach-notice"
	session.mu.Unlock()
	c, decoder, done := openCompactAttach(t, server, protocol.CellPaletteRunsV1)
	var initial protocol.Response
	if err := decoder.Decode(&initial); err != nil {
		t.Fatal(err)
	}
	if initial.Frame == nil || initial.Frame.Panes[0].Terminal.CompactRows == nil {
		t.Fatal("negotiated frame not packed")
	}
	if err := protocol.ExpandFrame(initial.Frame, protocol.CellPaletteRunsV1); err != nil {
		t.Fatal(err)
	}
	if initial.Frame.Notice != "first-attach-notice" {
		t.Fatal("transient notice dropped")
	}
	if err := protocol.EncodeRequest(json.NewEncoder(c), protocol.Request{Op: "resize", Width: 120, Height: 40, Seq: 7}); err != nil {
		t.Fatal(err)
	}
	for {
		var r protocol.Response
		if err := protocol.DecodeResponse(decoder, protocol.CellPaletteRunsV1, &r); err != nil {
			t.Fatal(err)
		}
		if r.Frame != nil && r.Frame.Width == 120 && r.Frame.Height == 40 {
			if r.Ack != 7 || len(r.Frame.Panes[0].Terminal.Rows) != 40 || len(r.Frame.Panes[0].Terminal.Rows[0]) != 120 {
				t.Fatal("resize or acknowledgement changed")
			}
			if r.Frame.Notice != "" {
				t.Fatal("one-shot notice repeated")
			}
			break
		}
	}
	if err := session.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	compactAttachFrame(t, decoder, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return len(f.Panes) == 2 })
	session.showMessage("transport-message")
	frame := compactAttachFrame(t, decoder, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return f.Message == "transport-message" })
	if frame.MessageSeq == 0 {
		t.Fatal("message sequence dropped")
	}
	if err := session.input(protocol.Request{Op: "zoom"}); err != nil {
		t.Fatal(err)
	}
	compactAttachFrame(t, decoder, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return f.Zoomed && len(f.Panes) == 1 })
	if err := session.input(protocol.Request{Op: "new-window"}); err != nil {
		t.Fatal(err)
	}
	compactAttachFrame(t, decoder, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return len(f.Windows) == 2 && !f.Zoomed })
	if err := protocol.EncodeRequest(json.NewEncoder(c), protocol.Request{Op: "detach"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("detach leaked handler")
	}
	c.Close()
	_, legacyDecoder, _ := openCompactAttach(t, server, "")
	frame = compactAttachFrame(t, legacyDecoder, "", func(f *protocol.Frame) bool { return len(f.Windows) == 2 })
	if frame.Notice != "" || frame.Message != "transport-message" {
		t.Fatal("reattach metadata changed")
	}
}

func TestCompactAndLegacyConnectionsKeepIndependentViews(t *testing.T) {
	server, session := viewportSession(t)
	if err := session.input(protocol.Request{Op: "paste", Text: "i=0; while [ $i -lt 80 ]; do echo COMPACT_HISTORY_$i; i=$((i+1)); done\n"}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, session.panes[1], func(f *protocol.TerminalFrame) bool {
		return f.History > 20 && strings.Contains(frameText(f), "COMPACT_HISTORY_79")
	})
	session.panes[1].apply(osc52("c", base64.StdEncoding.EncodeToString([]byte("before attachment"))))
	c1, d1, _ := openCompactAttach(t, server, protocol.CellPaletteRunsV1)
	c2, d2, _ := openCompactAttach(t, server, "")
	for _, reader := range []struct {
		dec      *json.Decoder
		encoding string
	}{{d1, protocol.CellPaletteRunsV1}, {d2, ""}} {
		frame := compactAttachFrame(t, reader.dec, reader.encoding, func(f *protocol.Frame) bool { return true })
		if frame.Clipboard != "" {
			t.Fatal("first frame replayed old clipboard")
		}
	}
	// Leave a legacy reader active while the compact connection scrolls.
	if err := protocol.EncodeRequest(json.NewEncoder(c1), protocol.Request{Op: "scroll", Delta: 10}); err != nil {
		t.Fatal(err)
	}
	scrolled := compactAttachFrame(t, d1, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return f.Panes[0].Terminal.Offset == 10 })
	if scrolled.Panes[0].Terminal.CursorVisible {
		t.Fatal("scrollback cursor changed")
	}
	if err := protocol.EncodeRequest(json.NewEncoder(c2), protocol.Request{Op: "scroll", Delta: 0}); err != nil {
		t.Fatal(err)
	}
	live := compactAttachFrame(t, d2, "", func(f *protocol.Frame) bool { return true })
	if live.Panes[0].Terminal.Offset != 0 {
		t.Fatal("connections shared scroll offsets")
	}
	session.panes[1].apply(osc52("c", base64.StdEncoding.EncodeToString([]byte("later clipboard"))))
	for _, reader := range []struct {
		dec      *json.Decoder
		encoding string
	}{{d1, protocol.CellPaletteRunsV1}, {d2, ""}} {
		compactAttachFrame(t, reader.dec, reader.encoding, func(f *protocol.Frame) bool { return f.Clipboard == "later clipboard" })
	}
	session.showMessage("after clipboard")
	for _, reader := range []struct {
		dec      *json.Decoder
		encoding string
	}{{d1, protocol.CellPaletteRunsV1}, {d2, ""}} {
		compactAttachFrame(t, reader.dec, reader.encoding, func(f *protocol.Frame) bool {
			if f.Clipboard != "" {
				t.Fatal("clipboard delivered twice")
			}
			return f.Message == "after clipboard"
		})
	}
}

func TestCompactAttachmentSessionExit(t *testing.T) {
	server, session := viewportSession(t)
	_, decoder, done := openCompactAttach(t, server, protocol.CellPaletteRunsV1)
	compactAttachFrame(t, decoder, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return true })
	if err := session.input(protocol.Request{Op: "paste", Text: "exit\n"}); err != nil {
		t.Fatal(err)
	}
	compactAttachFrame(t, decoder, protocol.CellPaletteRunsV1, func(f *protocol.Frame) bool { return len(f.Panes) == 0 })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("session exit left attachment running")
	}
}
