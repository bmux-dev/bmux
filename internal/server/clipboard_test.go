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

func osc52(target, payload string) []byte {
	return []byte("\x1b]52;" + target + ";" + payload + "\a")
}

func TestClipboardWritesFromAppsAreValidatedBeforeKeeping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("clip", t.TempDir(), "/bin/sh", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	kept := func() (string, uint64) {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.clipboard, p.clipboardSeq
	}
	for name, seq := range map[string][]byte{
		"query":         osc52("c", "?"),
		"clear":         osc52("c", ""),
		"bad base64":    osc52("c", "not base64!"),
		"invalid UTF-8": osc52("c", base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})),
		// Fills the emulator's OSC buffer, so it may have been cut short.
		"maybe truncated": osc52("c", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 3<<20)))),
	} {
		p.apply(seq)
		if text, n := kept(); n != 0 {
			t.Fatalf("%s kept %d bytes", name, len(text))
		}
	}
	// Neovim's "+" and "*" registers both reach the clipboard.
	for target, text := range map[string]string{"c": "yanked line\n", "p": "primary ünïcode", "": "default target"} {
		p.apply(osc52(target, base64.StdEncoding.EncodeToString([]byte(text))))
		if got, _ := kept(); got != text {
			t.Fatalf("target %q kept %q, want %q", target, got, text)
		}
	}
	p.apply(osc52("c", base64.RawStdEncoding.EncodeToString([]byte("no padding"))))
	if got, _ := kept(); got != "no padding" {
		t.Fatalf("unpadded base64 kept %q", got)
	}
}

func TestAttachmentsDeliverEachLaterClipboardWriteOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New("/bin/sh")
	finished := make(chan error, 1)
	go func() { finished <- srv.Serve(l) }()
	defer func() {
		srv.Close()
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if r := srv.request(protocol.Request{Op: "create", Name: "clip", Dir: t.TempDir(), Width: 80, Height: 24}); r.Error != "" {
		t.Fatal(r.Error)
	}
	s := srv.sessions["clip"]
	// Pane 2 exists but isn't shown: pane 1 is zoomed.
	for _, r := range []protocol.Request{{Op: "split", Direction: "horizontal"}, {Op: "select-pane", PaneID: 1}, {Op: "zoom"}} {
		if err := s.input(r); err != nil {
			t.Fatal(err)
		}
	}
	s.panes[1].apply(osc52("c", base64.StdEncoding.EncodeToString([]byte("before attaching"))))

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "clip", Width: 80, Height: 24}); err != nil {
		t.Fatal(err)
	}
	var r protocol.Response
	if err := dec.Decode(&r); err != nil || r.Error != "" {
		t.Fatalf("attach: %+v %v", r, err)
	}
	frames := make(chan *protocol.Frame, 64)
	go func() {
		for {
			var r protocol.Response
			if dec.Decode(&r) != nil {
				close(frames)
				return
			}
			if r.Frame != nil {
				frames <- r.Frame
			}
		}
	}()
	next := func() *protocol.Frame {
		t.Helper()
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("attachment closed")
			}
			return f
		case <-time.After(5 * time.Second):
			t.Fatal("no frame")
			return nil
		}
	}
	if f := next(); f.Clipboard != "" {
		t.Fatalf("first frame replayed an earlier copy: %q", f.Clipboard)
	}
	s.panes[2].apply(osc52("c", base64.StdEncoding.EncodeToString([]byte("from the hidden pane"))))
	for f := next(); f.Clipboard != "from the hidden pane"; f = next() {
		if f.Clipboard != "" {
			t.Fatalf("delivered %q", f.Clipboard)
		}
	}
	s.panes[1].apply([]byte("more output\r\n"))
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case f := <-frames:
			if f != nil && f.Clipboard != "" {
				t.Fatalf("copy delivered again: %q", f.Clipboard)
			}
			continue
		case <-deadline:
		}
		break
	}
}
