package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func navigationUI(t *testing.T) (*ui, <-chan protocol.Request) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	u := &ui{conn: &connection{Conn: a, enc: json.NewEncoder(a)}, focused: 1}
	requests := make(chan protocol.Request, 32)
	go func() {
		defer close(requests)
		dec := json.NewDecoder(b)
		for {
			var r protocol.Request
			if dec.Decode(&r) != nil {
				return
			}
			requests <- r
		}
	}()
	return u, requests
}

func nextNavigationRequest(t *testing.T, requests <-chan protocol.Request, op, direction string) protocol.Request {
	t.Helper()
	select {
	case r := <-requests:
		if r.Op != op || r.Direction != direction {
			t.Fatalf("got %+v, want %s %s", r, op, direction)
		}
		return r
	case <-time.After(time.Second):
		t.Fatal("missing request")
	}
	return protocol.Request{}
}

func TestBareControlHJKLUseEditorAwareNavigation(t *testing.T) {
	u, requests := navigationUI(t)
	for _, tc := range []struct {
		key       tcell.Key
		direction string
	}{{tcell.KeyCtrlH, "left"}, {tcell.KeyCtrlJ, "down"}, {tcell.KeyCtrlK, "up"}, {tcell.KeyCtrlL, "right"}} {
		u.key(tcell.NewEventKey(tc.key, 0, tcell.ModCtrl))
		r := nextNavigationRequest(t, requests, "navigate-pane", tc.direction)
		if r.PaneID != 0 || r.Seq == 0 {
			t.Fatalf("invalid directional focus request: %+v", r)
		}
	}
	// Modifiers outside bare Ctrl must not steal application input.
	u.key(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModCtrl|tcell.ModAlt))
	nextNavigationRequest(t, requests, "key", "")
	u.key(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone))
	if r := nextNavigationRequest(t, requests, "key", ""); r.Code != uv.KeyBackspace {
		t.Fatalf("backspace became navigation: %+v", r)
	}
	u.key(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if r := nextNavigationRequest(t, requests, "key", ""); r.Code != uv.KeyEnter {
		t.Fatalf("Enter became navigation: %+v", r)
	}
}

func TestResizeRepeatWindowRenewsAndExpires(t *testing.T) {
	u, requests := navigationUI(t)
	start := time.Unix(100, 0)
	u.keyAt(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl), start)
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone), start)
	nextNavigationRequest(t, requests, "resize-pane", "left")
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone), start.Add(900*time.Millisecond))
	nextNavigationRequest(t, requests, "resize-pane", "down")
	// The window renews after each resize; this is >1s after the first press.
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone), start.Add(1800*time.Millisecond))
	nextNavigationRequest(t, requests, "resize-pane", "up")
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'l', tcell.ModNone), start.Add(2800*time.Millisecond))
	r := nextNavigationRequest(t, requests, "key", "")
	if r.Text != "l" || !u.resizeUntil.IsZero() {
		t.Fatalf("repeat didn't expire: %+v", r)
	}
}

func TestOtherInputEndsResizeRepeatWithoutBeingSwallowed(t *testing.T) {
	for _, event := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModAlt),
		tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl),
		tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone),
	} {
		u, requests := navigationUI(t)
		start := time.Unix(100, 0)
		u.resizeUntil = start.Add(time.Second)
		u.keyAt(event, start)
		op, direction := "key", ""
		if event.Key() == tcell.KeyCtrlK {
			op, direction = "navigate-pane", "up"
		}
		nextNavigationRequest(t, requests, op, direction)
		u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone), start.Add(time.Millisecond))
		if r := nextNavigationRequest(t, requests, "key", ""); r.Text != "h" {
			t.Fatal("resize repetition survived unrelated input")
		}
	}
}

func TestPrefixArrowsAlwaysSelectbmuxPane(t *testing.T) {
	u, requests := navigationUI(t)
	u.key(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModCtrl))
	u.key(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone))
	r := nextNavigationRequest(t, requests, "select-pane", "right")
	if r.PaneID != 0 || r.Seq == 0 {
		t.Fatal("explicit prefix navigation lost focus fencing")
	}
}

func TestPickerAndPasteDoNotInterpretNavigationOrResize(t *testing.T) {
	start := time.Unix(100, 0)
	u := &ui{picker: true, resizeUntil: start.Add(time.Second)}
	u.keyAt(tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModNone), start)
	if u.query != "" || !u.resizeUntil.IsZero() {
		t.Fatal("list picker input interpreted as resizing or search")
	}
	u, requests := navigationUI(t)
	u.resizeUntil = start.Add(time.Second)
	u.handleRawPaste(&rawPasteEvent{Text: "\x08\x0a\x0b\x0ch"})
	r := nextNavigationRequest(t, requests, "paste", "")
	if r.Text != "\x08\x0a\x0b\x0ch" || !u.resizeUntil.IsZero() {
		t.Fatalf("paste not literal: %+v", r)
	}
}

func TestNavigationReaderPreservesBackspaceAndFragmentedReads(t *testing.T) {
	for _, size := range []int{1, 2, 32} {
		r := &navigationReader{reader: bytes.NewBufferString("a\x08b\x7fc\x08")}
		var result bytes.Buffer
		buf := make([]byte, size)
		for {
			n, err := r.Read(buf)
			result.Write(buf[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if got := result.String(); got != "a\x1b[104;5ub\x7fc\x1b[104;5u" {
			t.Fatalf("buffer %d: %q", size, got)
		}
	}
}
