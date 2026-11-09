package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/vt"
)

func TestFramePublicationSparseChangesAreImmediate(t *testing.T) {
	now := time.Unix(1000, 0)
	p := framePublication{dirty: true}
	for i := 0; i < 100; i++ {
		if delay, dirty := p.delay(now); !dirty || delay != 0 {
			t.Fatalf("sparse change waits %v (dirty=%v)", delay, dirty)
		}
		p.dirty = false // consume before snapshot; only a real frame calls sent
		p.sent(now)
		now = now.Add(frameInterval + time.Duration(i+1)*time.Millisecond)
		if _, dirty := p.delay(now); dirty {
			t.Fatal("idle publication would poll")
		}
		p.dirty = true
	}
}

func TestFramePublicationSustainedCapAndBoundedTrailingFrame(t *testing.T) {
	start := time.Unix(1000, 0)
	p := framePublication{}
	var sent []time.Time
	// Model continuous changes with explicit timestamps, not timer/sleep tolerances.
	for now := start; now.Before(start.Add(time.Second)); now = now.Add(time.Millisecond) {
		p.dirty = true
		if delay, _ := p.delay(now); delay == 0 {
			p.dirty = false
			p.sent(now)
			sent = append(sent, now)
		}
	}
	if len(sent) != 30 {
		t.Fatalf("sent %d frames in a half-open second, want 30", len(sent))
	}
	for i := 1; i < len(sent); i++ {
		if gap := sent[i].Sub(sent[i-1]); gap < frameInterval {
			t.Fatalf("frames only %v apart", gap)
		}
	}
	// Continued notifications never postpone this deadline. When output stops,
	// the last pending change is still eligible at that deadline without a wake.
	deadline := p.nextAllowed
	for now := sent[len(sent)-1].Add(time.Millisecond); now.Before(deadline); now = now.Add(time.Millisecond) {
		p.dirty = true
		if delay, dirty := p.delay(now); !dirty || now.Add(delay) != deadline {
			t.Fatalf("trailing deadline moved: delay=%v dirty=%v", delay, dirty)
		}
	}
	if delay, dirty := p.delay(deadline); delay != 0 || !dirty {
		t.Fatalf("final change not eligible: %v %v", delay, dirty)
	}
	p.dirty = false
	// A stale hint/nil snapshot must not restart the cooldown.
	p.dirty = true
	if delay, _ := p.delay(deadline); delay != 0 {
		t.Fatalf("nil snapshot consumed the budget: %v", delay)
	}
	// Slow writes anchor the next deadline to completion, not an overdue tick.
	p.sent(deadline.Add(2 * frameInterval))
	if delay, _ := p.delay(deadline.Add(2 * frameInterval)); delay != frameInterval {
		t.Fatalf("slow write permits catch-up burst: %v", delay)
	}
}

func TestFramePublicationExactDeadlinesRespectCap(t *testing.T) {
	start := time.Unix(1000, 0)
	p := framePublication{dirty: true}
	frames := 0
	for now := start; now.Before(start.Add(time.Second)); now = p.nextAllowed {
		if delay, dirty := p.delay(now); delay != 0 || !dirty {
			t.Fatalf("deadline is not eligible: %v %v", delay, dirty)
		}
		p.sent(now)
		frames++
	}
	if frames != 30 {
		t.Fatalf("exact deadlines sent %d frames in one second, want 30", frames)
	}
}

func TestFrameChangesBroadcastCoalescesAndRetainsLaterWake(t *testing.T) {
	var changes frameChanges
	a, b := changes.subscribe(), changes.subscribe()
	defer changes.unsubscribe(a)
	defer changes.unsubscribe(b)
	for i := 0; i < 100; i++ {
		changes.notify()
	}
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("not coalesced for both subscribers: %d %d", len(a), len(b))
	}
	<-a              // consumed before snapshot
	changes.notify() // state changes during snapshot/send
	if len(a) != 1 || len(b) != 1 {
		t.Fatal("slow subscriber blocked or consumed another subscriber's wake")
	}
	changes.unsubscribe(b)
	<-b
	changes.notify()
	if len(b) != 0 {
		t.Fatal("detached subscriber notified")
	}
}

func TestFrameChangesConcurrentDetach(t *testing.T) {
	var changes frameChanges
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < 1000; i++ {
			changes.notify()
		}
	})
	for i := 0; i < 1000; i++ {
		wake := changes.subscribe()
		changes.unsubscribe(wake)
	}
	wg.Wait()
}

// In-memory panes have real emulator/input bridges but no OS processes. This
// isolates publication controls from shell startup, PTY timing and agent sampling.
func publicationFixture(t *testing.T) (*session, *pane, *pane) {
	t.Helper()
	s := &session{
		info:  protocol.Session{Name: "publication", Created: time.Now()},
		panes: make(map[int]*pane), width: 20, height: 4, revision: 1,
		windows: []*window{{root: &layout{id: 1}, focused: 1}, {root: &layout{id: 2}, focused: 2}},
	}
	for id := 1; id <= 2; id++ {
		p := &pane{term: vt.NewEmulator(20, 4), epoch: 1, revision: 1, visible: true,
			modes: make(map[int]bool), frameChanged: s.frameChanges.notify}
		p.trackClipboard()
		p.bridge = newInputBridge(p.term, io.Discard)
		t.Cleanup(func() {
			p.bridge.close(io.ErrClosedPipe)
			_ = p.term.InputPipe().(io.Closer).Close()
			<-p.bridge.drainDone
			<-p.bridge.writerDone
		})
		s.panes[id] = p
	}
	s.refreshFramePanesLocked()
	return s, s.panes[1], s.panes[2]
}

func TestPanePublicationIsOutputDrivenAndFiltersBackground(t *testing.T) {
	s, foreground, background := publicationFixture(t)
	wake := s.frameChanges.subscribe()
	defer s.frameChanges.unsubscribe(wake)
	for _, r := range []protocol.Request{
		{Op: "key", Code: 'x', Text: "x"}, {Op: "paste", Text: "pasted"},
		{Op: "type", Text: "typed"}, {Op: "focus", Focused: true},
	} {
		if err := foreground.input(r); err != nil {
			t.Fatal(err)
		}
	}
	if len(wake) != 0 {
		t.Fatal("input spent the publication budget before its echo")
	}
	background.apply([]byte("hidden"))
	if len(wake) != 0 {
		t.Fatal("background output woke publication")
	}
	foreground.apply([]byte("echo"))
	if len(wake) != 1 {
		t.Fatal("applied echo did not wake publication")
	}
	<-wake
	foreground.info.Exited = true // allow emulator-only resize without a PTY
	if err := foreground.input(protocol.Request{Op: "resize", Width: 20, Height: 4}); err != nil {
		t.Fatal(err)
	}
	if len(wake) != 1 {
		t.Fatal("resize did not wake publication")
	}
	<-wake
	s.mu.Lock()
	s.active = 1
	s.refreshFramePanesLocked()
	s.mu.Unlock()
	foreground.apply([]byte("now hidden"))
	background.apply([]byte("now active"))
	if len(wake) != 1 || foreground.frameActive.Load() || !background.frameActive.Load() {
		t.Fatal("window switch did not update publication membership")
	}
	<-wake
	// Match revisionLocked: zoom hides cells but not active-window membership.
	s.mu.Lock()
	s.windows[1].root = &layout{first: &layout{id: 1}, second: &layout{id: 2}, ratio: 5000}
	s.windows[1].zoomed = true
	s.refreshFramePanesLocked()
	s.mu.Unlock()
	foreground.apply([]byte("under zoom"))
	if len(wake) != 1 {
		t.Fatal("zoom-hidden active pane stopped contributing")
	}
}

type publicationSendingConn struct {
	net.Conn
	writes  int
	sending chan struct{}
}

func (c *publicationSendingConn) Write(data []byte) (int, error) {
	c.writes++
	if c.writes == 2 { // handshake is first; initial frame has already been sampled
		close(c.sending)
	}
	return c.Conn.Write(data)
}

func publicationAttachment(t *testing.T, srv *Server, sending chan struct{}) (*json.Decoder, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	var conn net.Conn = server
	if sending != nil {
		conn = &publicationSendingConn{Conn: server, sending: sending}
	}
	done := make(chan struct{})
	go func() { defer close(done); defer server.Close(); srv.handle(conn) }()
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("attachment did not exit")
		}
	})
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	if err := json.NewEncoder(client).Encode(protocol.Request{
		Version: protocol.Version, Op: "attach", Name: "publication", Width: 20, Height: 4,
	}); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(client)
	var response protocol.Response
	if err := dec.Decode(&response); err != nil || response.Error != "" {
		t.Fatalf("attach: %+v %v", response, err)
	}
	return dec, client
}

func publicationNextFrame(t *testing.T, dec *json.Decoder) *protocol.Frame {
	t.Helper()
	var r protocol.Response
	if err := dec.Decode(&r); err != nil || r.Error != "" || r.Frame == nil {
		t.Fatalf("frame: %+v %v", r, err)
	}
	return r.Frame
}

func TestAttachmentRetainsOutputDuringSendAndPublishesTrailingFrame(t *testing.T) {
	s, p, background := publicationFixture(t)
	// With no live PTY, resize skips ioctl; output is applied directly below.
	p.info.Exited, background.info.Exited = true, true
	srv := New("/bin/sh")
	srv.sessions[s.info.Name] = s
	sending := make(chan struct{})
	dec, _ := publicationAttachment(t, srv, sending)
	select {
	case <-sending:
	case <-time.After(5 * time.Second):
		t.Fatal("initial publication did not start")
	}
	// The initial frame was sampled, but net.Pipe blocks its send until decoded.
	// No further output follows: the cooldown timer must deliver this final state.
	p.apply([]byte("FINAL"))
	if f := publicationNextFrame(t, dec); strings.Contains(frameText(f.Panes[0].Terminal), "FINAL") {
		t.Fatal("initial frame was not isolated from the later mutation")
	}
	if f := publicationNextFrame(t, dec); !strings.Contains(frameText(f.Panes[0].Terminal), "FINAL") {
		t.Fatal("output during send was lost")
	}
}

func TestAttachmentsReceiveMetadataAndBackgroundClipboardOnNextFrame(t *testing.T) {
	s, p, background := publicationFixture(t)
	p.info.Exited, background.info.Exited = true, true
	srv := New("/bin/sh")
	srv.sessions[s.info.Name] = s
	a, _ := publicationAttachment(t, srv, nil)
	publicationNextFrame(t, a)
	b, _ := publicationAttachment(t, srv, nil)
	publicationNextFrame(t, b)
	// Preserve background semantics: its copy rides the next eligible frame,
	// rather than causing a new class of unsolicited background publications.
	background.apply(osc52("c", base64.StdEncoding.EncodeToString([]byte("background copy"))))
	s.showMessage("wake both")
	for _, dec := range []*json.Decoder{a, b} {
		copied := false
		for {
			f := publicationNextFrame(t, dec)
			if f.Clipboard != "" {
				if copied || f.Clipboard != "background copy" {
					t.Fatalf("wrong/repeated background clipboard: %q", f.Clipboard)
				}
				copied = true
			}
			if f.Message == "wake both" {
				if !copied {
					t.Fatal("background clipboard missing")
				}
				break
			}
		}
	}
	// Config/update publication uses this same session revision-bump path.
	srv.bumpRevisions()
	publicationNextFrame(t, a)
	publicationNextFrame(t, b)
	if r := srv.request(protocol.Request{Op: "rename", Name: "publication", NewName: "renamed"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	for _, dec := range []*json.Decoder{a, b} {
		if f := publicationNextFrame(t, dec); f.Name != "renamed" || f.Clipboard != "" {
			t.Fatalf("rename/repeated clipboard: %+v", f)
		}
	}
	s.mu.Lock()
	// Pane removal carries its revision, as watchPane does, so the final
	// snapshot cannot equal the previous active-window revision.
	s.revision += p.revision + 1
	s.windows = nil
	s.panes = make(map[int]*pane)
	s.frameChanges.notify()
	s.mu.Unlock()
	for _, dec := range []*json.Decoder{a, b} {
		if f := publicationNextFrame(t, dec); len(f.Panes) != 0 {
			t.Fatal("final empty frame not published")
		}
	}
}
