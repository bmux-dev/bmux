package server

import (
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bmux-dev/bmux/internal/config"
	"github.com/bmux-dev/bmux/internal/graphics"
	"github.com/bmux-dev/bmux/internal/protocol"
	"github.com/bmux-dev/bmux/internal/vt"
)

func newTestPane(t *testing.T, w, h int) *pane {
	t.Helper()
	p := &pane{
		info:     protocol.Session{Name: "test"},
		term:     vt.NewEmulator(w, h),
		visible:  true,
		blink:    true,
		modes:    make(map[int]bool),
		done:     make(chan struct{}),
		stopDone: make(chan struct{}),
		revision: 1,
		epoch:    1,
	}
	p.term.SetScrollbackSize(currentSettings().scrollback)
	p.term.SetScrollbackMaxBytes(currentSettings().scrollbackMaxBytes)
	p.term.SetCallbacks(vt.Callbacks{
		Title:            func(title string) { p.title = title },
		CursorVisibility: func(v bool) { p.visible = v },
		CursorStyle:      func(style vt.CursorStyle, steady bool) { p.style, p.blink = style, !steady },
		AltScreen:        func(bool) { p.epoch++ },
	})
	p.trackBackground()
	return p
}

func assertFramesMatch(t *testing.T, label string, got, want *protocol.TerminalFrame) {
	t.Helper()
	if got.Delta || got.Unchanged || got.ScrollUp != 0 || got.RowDeltas != nil {
		t.Fatalf("%s: reconstructed frame still has delta fields: %+v", label, got)
	}
	if got.Name != want.Name || got.Title != want.Title ||
		got.Base != want.Base || got.Offset != want.Offset ||
		got.History != want.History || got.HistoryStart != want.HistoryStart ||
		got.Epoch != want.Epoch || got.Alt != want.Alt || got.Exited != want.Exited ||
		got.Mouse != want.Mouse ||
		got.CursorX != want.CursorX || got.CursorY != want.CursorY ||
		got.CursorVisible != want.CursorVisible || got.CursorStyle != want.CursorStyle ||
		got.CursorBlink != want.CursorBlink {
		t.Fatalf("%s: metadata mismatch:\ngot:  %+v\nwant: %+v", label, got, want)
	}
	if !reflect.DeepEqual(got.Rows, want.Rows) {
		t.Fatalf("%s: rows mismatch:\ngot:  %q\nwant: %q", label, frameText(got), frameText(want))
	}
	if !reflect.DeepEqual(got.Wrapped, want.Wrapped) {
		t.Fatalf("%s: wrapped mismatch: got %v, want %v", label, got.Wrapped, want.Wrapped)
	}
	if !reflect.DeepEqual(got.Images, want.Images) {
		t.Fatalf("%s: images mismatch: got %+v, want %+v", label, got.Images, want.Images)
	}
}

func TestPaneDeltaStateMachine(t *testing.T) {
	p := newTestPane(t, 20, 6)
	state := &paneDeltaState{}
	var clientFrame *protocol.TerminalFrame

	write := func(seq string) {
		p.mu.Lock()
		p.trackHistory(func() { _, _ = p.term.WriteString(seq) })
		p.revision++
		p.mu.Unlock()
	}

	step := func(label string, offset int) *protocol.TerminalFrame {
		t.Helper()
		p.mu.Lock()
		delta, _ := p.frameDeltaLocked(offset, state)
		full, _ := p.frameLocked(offset)
		p.mu.Unlock()
		clientFrame = protocol.ApplyTerminalDelta(clientFrame, delta)
		assertFramesMatch(t, label, clientFrame, full)
		return delta
	}

	// 1. Initial frame is always full.
	write("line0\r\nline1\r\nline2")
	d := step("initial", 0)
	if d.Delta {
		t.Fatalf("initial frame must be full, got Delta=true")
	}

	// 2. No changes -> Delta: true, Unchanged: true.
	d = step("unchanged", 0)
	if !d.Delta || !d.Unchanged || len(d.Rows) != 0 || len(d.RowDeltas) != 0 {
		t.Fatalf("expected Unchanged delta, got %+v", d)
	}

	// 3. Single-row edit -> 1 RowDelta.
	write("\x1b[2;1HEDIT1")
	d = step("single-row-edit", 0)
	if !d.Delta || d.Unchanged || d.ScrollUp != 0 || len(d.RowDeltas) != 1 || d.RowDeltas[0].Y != 1 {
		t.Fatalf("expected 1 RowDelta at Y=1, got delta=%v unchanged=%v scroll=%d deltas=%d", d.Delta, d.Unchanged, d.ScrollUp, len(d.RowDeltas))
	}

	// 4. Cursor-only move -> Delta: true, 0 RowDeltas.
	write("\x1b[4;7H")
	d = step("cursor-only", 0)
	if !d.Delta || d.Unchanged || d.ScrollUp != 0 || len(d.RowDeltas) != 0 || d.CursorX != 6 || d.CursorY != 3 {
		t.Fatalf("expected metadata-only cursor delta, got %+v", d)
	}

	// 5. Fill to bottom then scroll up by 2 lines -> ScrollUp: 2 + bottom RowDeltas.
	write("\x1b[6;1Hline5")
	_ = step("fill-bottom", 0)
	write("\r\nline6\r\nline7")
	d = step("scroll-up-2", 0)
	if !d.Delta || d.Unchanged || d.ScrollUp != 2 {
		t.Fatalf("expected ScrollUp=2 delta, got delta=%v unchanged=%v scroll=%d", d.Delta, d.Unchanged, d.ScrollUp)
	}
	if len(d.RowDeltas) > 3 {
		t.Fatalf("expected at most 3 dirty bottom rows on 2-line scroll, got %d", len(d.RowDeltas))
	}

	// 6. Scroll up 1 line AND mutate row 0 in the same batch -> ScrollUp: 1 + RowDeltas includes row 0.
	write("\x1b[6;1H\r\nline8\x1b[1;1HTOP!")
	d = step("scroll-and-top-edit", 0)
	if !d.Delta || d.ScrollUp != 1 {
		t.Fatalf("expected ScrollUp=1 delta, got delta=%v scroll=%d", d.Delta, d.ScrollUp)
	}

	// 7. Scrollback offset > 0 -> full frame, then unchanged while scrolled back.
	d = step("scrollback-view", 2)
	if d.Delta {
		t.Fatalf("scrollback offset transition should send full frame, got Delta=true")
	}
	d = step("scrollback-unchanged", 2)
	if !d.Delta || !d.Unchanged {
		t.Fatalf("unchanged scrolled-back pane should send Unchanged=true, got %+v", d)
	}
	// Back to live viewport -> full frame.
	d = step("back-to-live", 0)
	if d.Delta {
		t.Fatalf("return to live viewport should send full frame, got Delta=true")
	}

	// 8. Alt screen toggle -> full frame, then single-row delta inside alt screen.
	write("\x1b[?1049hALT_ROW0")
	d = step("enter-alt", 0)
	if d.Delta || !d.Alt {
		t.Fatalf("enter alt screen should send full frame with Alt=true, got %+v", d)
	}
	write("\x1b[3;1HALT_ROW2")
	d = step("alt-single-row", 0)
	if !d.Delta || d.Unchanged || len(d.RowDeltas) != 1 || d.RowDeltas[0].Y != 2 {
		t.Fatalf("alt screen single-row edit should send 1 RowDelta at Y=2, got %+v", d)
	}
	write("\x1b[?1049l")
	d = step("leave-alt", 0)
	if d.Delta || d.Alt {
		t.Fatalf("leave alt screen should send full frame with Alt=false, got %+v", d)
	}

	// 9. OSC 11 background change -> full frame so default-BG cells repaint.
	write("\x1b]11;#102030\x07")
	d = step("osc11-bg", 0)
	if d.Delta {
		t.Fatalf("OSC 11 background change should trigger full frame, got Delta=true")
	}

	// 10. OSC 4 palette redefinition -> full frame.
	write("\x1b]4;1;rgb:ff/00/88\x07")
	d = step("osc4-palette", 0)
	if d.Delta {
		t.Fatalf("OSC 4 palette change should trigger full frame, got Delta=true")
	}

	// 11. Resize -> full frame.
	p.mu.Lock()
	p.trackHistory(func() { p.term.Resize(24, 8) })
	p.epoch++
	p.historyTotal = p.term.ScrollbackLen()
	p.revision++
	p.mu.Unlock()
	d = step("resize", 0)
	if d.Delta {
		t.Fatalf("resize should send full frame, got Delta=true")
	}
}

func TestPaneDeltaOSC8Links(t *testing.T) {
	p := newTestPane(t, 30, 5)
	state := &paneDeltaState{}
	var clientFrame *protocol.TerminalFrame

	write := func(seq string) {
		p.mu.Lock()
		p.trackHistory(func() { _, _ = p.term.WriteString(seq) })
		p.revision++
		p.mu.Unlock()
	}

	write("\x1b[1;1H\x1b]8;;https://first.example\x07one\x1b]8;;\x07")
	p.mu.Lock()
	f1, _ := p.frameDeltaLocked(0, state)
	p.mu.Unlock()
	clientFrame = protocol.ApplyTerminalDelta(clientFrame, f1)

	// Edit row 3 with a second hyperlink; row 0 is untouched.
	write("\x1b[4;1H\x1b]8;;https://second.example\x07two\x1b]8;;\x07")
	p.mu.Lock()
	f2, _ := p.frameDeltaLocked(0, state)
	p.mu.Unlock()
	if !f2.Delta || len(f2.RowDeltas) != 1 || f2.RowDeltas[0].Y != 3 {
		t.Fatalf("expected 1 RowDelta at Y=3, got %+v", f2)
	}
	clientFrame = protocol.ApplyTerminalDelta(clientFrame, f2)

	link0 := clientFrame.Rows[0][0].Link
	link3 := clientFrame.Rows[3][0].Link
	if link0 <= 0 || link0 > len(clientFrame.Links) || clientFrame.Links[link0-1] != "https://first.example" {
		t.Fatalf("unchanged row 0 link broken after delta: idx=%d links=%v", link0, clientFrame.Links)
	}
	if link3 <= 0 || link3 > len(clientFrame.Links) || clientFrame.Links[link3-1] != "https://second.example" {
		t.Fatalf("dirty row 3 link broken after delta: idx=%d links=%v", link3, clientFrame.Links)
	}
}

func TestPaneDeltaKittyGraphics(t *testing.T) {
	store := graphics.NewStore(graphics.DefaultLimits)
	p := newTestPane(t, 20, 6)
	p.graphicsOK.Store(true)
	p.trackGraphics(store)
	defer p.graphics.Close()

	state := &paneDeltaState{}
	var clientFrame *protocol.TerminalFrame

	write := func(seq string) {
		p.mu.Lock()
		p.trackHistory(func() { _, _ = p.term.WriteString(seq) })
		p.revision++
		p.mu.Unlock()
	}

	// Transmit + place a 1x1 RGB image (id=7) and draw a placeholder cell on row 0.
	write("\x1b_Ga=t,f=24,s=1,v=1,i=7,q=2;////\x1b\\")
	write("\x1b_Ga=p,U=1,i=7,c=2,r=1,q=2\x1b\\")
	write("\x1b[1;1H\x1b[38;2;0;0;7m" + vt.Placeholder + "\x1b[0m")

	p.mu.Lock()
	d1, _ := p.frameDeltaLocked(0, state)
	p.mu.Unlock()
	clientFrame = protocol.ApplyTerminalDelta(clientFrame, d1)
	if len(clientFrame.Images) != 1 || clientFrame.Rows[0][0].Text != vt.Placeholder {
		t.Fatalf("expected 1 image and placeholder cell, got images=%+v cell=%+v", clientFrame.Images, clientFrame.Rows[0][0])
	}

	// Unchanged image pane still emits Unchanged: true.
	p.mu.Lock()
	d2, _ := p.frameDeltaLocked(0, state)
	p.mu.Unlock()
	if !d2.Delta || !d2.Unchanged {
		t.Fatalf("unchanged image pane should emit Unchanged=true, got %+v", d2)
	}

	// Deleting the image (without touching screen cells) must trigger full frame and blank the placeholder.
	write("\x1b_Ga=d,d=I,i=7,q=2\x1b\\")
	p.mu.Lock()
	d3, _ := p.frameDeltaLocked(0, state)
	p.mu.Unlock()
	if d3.Delta {
		t.Fatalf("deleted image should fall back to full frame to blank placeholders, got Delta=true")
	}
	clientFrame = protocol.ApplyTerminalDelta(clientFrame, d3)
	if len(clientFrame.Images) != 0 || clientFrame.Rows[0][0].Text != " " {
		t.Fatalf("deleted image placeholder not blanked: images=%+v cell=%+v", clientFrame.Images, clientFrame.Rows[0][0])
	}

	// Now that no images remain, subsequent edits return to delta mode.
	write("\x1b[3;1Hplain")
	p.mu.Lock()
	d4, _ := p.frameDeltaLocked(0, state)
	p.mu.Unlock()
	if !d4.Delta || d4.Unchanged || len(d4.RowDeltas) != 1 || d4.RowDeltas[0].Y != 2 {
		t.Fatalf("expected return to delta mode after image cleared, got %+v", d4)
	}
}

func TestSessionMultiPaneDeltaAndWindowSwitch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("delta-split", t.TempDir(), "/bin/sh", 40, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()

	if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}

	offsets, histories := make(map[int]int), make(map[int]int)
	states := make(map[int]*paneDeltaState)
	f1, rev := s.snapshotDelta(offsets, histories, states, 0, true)
	if len(f1.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(f1.Panes))
	}
	for _, pf := range f1.Panes {
		if pf.Terminal.Delta {
			t.Fatalf("initial pane %d should be full frame", pf.ID)
		}
	}

	// Mutate only pane 1; pane 2 must come back Unchanged.
	s.mu.Lock()
	p1 := s.panes[1]
	s.mu.Unlock()
	p1.apply([]byte("\x1b[2;1HONLY_PANE_1"))

	f2, rev := s.snapshotDelta(offsets, histories, states, rev, false)
	if f2 == nil || len(f2.Panes) != 2 {
		t.Fatalf("expected 2-pane delta snapshot, got %+v", f2)
	}
	var got1, got2 *protocol.TerminalFrame
	for _, pf := range f2.Panes {
		if pf.ID == 1 {
			got1 = pf.Terminal
		} else if pf.ID == 2 {
			got2 = pf.Terminal
		}
	}
	if !got1.Delta || got1.Unchanged || len(got1.RowDeltas) == 0 {
		t.Fatalf("pane 1 expected row delta, got %+v", got1)
	}
	if !got2.Delta || !got2.Unchanged {
		t.Fatalf("pane 2 expected Unchanged=true, got %+v", got2)
	}

	// Switch to a new window and back: hidden panes are pruned from states so
	// switching back always sends full frames.
	if err := s.input(protocol.Request{Op: "new-window"}); err != nil {
		t.Fatal(err)
	}
	fWin2, rev := s.snapshotDelta(offsets, histories, states, rev, false)
	if len(fWin2.Panes) != 1 || fWin2.Panes[0].Terminal.Delta {
		t.Fatalf("new window expected 1 full pane, got %+v", fWin2.Panes)
	}
	if len(states) != 1 {
		t.Fatalf("expected states pruned to 1 active pane, got %d", len(states))
	}

	if err := s.input(protocol.Request{Op: "select-window", Window: 1}); err != nil {
		t.Fatal(err)
	}
	fBack, _ := s.snapshotDelta(offsets, histories, states, rev, false)
	if len(fBack.Panes) != 2 {
		t.Fatalf("window 1 expected 2 panes, got %d", len(fBack.Panes))
	}
	for _, pf := range fBack.Panes {
		if pf.Terminal.Delta {
			t.Fatalf("returning to window 1 pane %d must send full frame, got Delta=true", pf.ID)
		}
	}
}

func TestTrackHistoryUsesScrollbackTotalWithoutLimitHack(t *testing.T) {
	cfg := config.Default()
	cfg.Scrollback.Lines = 50
	activeSettings.Store(settingsFrom(cfg))
	t.Cleanup(func() { activeSettings.Store(settingsFrom(config.Default())) })

	p := newTestPane(t, 20, 4)
	p.mu.Lock()
	defer p.mu.Unlock()
	if got := p.term.Scrollback().MaxLines(); got != 50 {
		t.Fatalf("initial MaxLines = %d, want 50", got)
	}
	var out strings.Builder
	for i := range 200 {
		fmt.Fprintf(&out, "L%04d\r\n", i)
	}
	epochBefore := p.epoch
	p.trackHistory(func() { _, _ = p.term.WriteString(out.String()) })
	if p.epoch != epochBefore {
		t.Fatalf("normal scrollback eviction should not bump epoch: %d -> %d", epochBefore, p.epoch)
	}
	if got := p.term.Scrollback().MaxLines(); got != 50 {
		t.Fatalf("MaxLines changed unexpectedly: %d", got)
	}
	if p.term.ScrollbackLen() != 50 {
		t.Fatalf("ScrollbackLen = %d, want 50", p.term.ScrollbackLen())
	}
	if p.historyTotal != int(p.term.ScrollbackTotal()) {
		t.Fatalf("historyTotal = %d, want ScrollbackTotal() = %d", p.historyTotal, p.term.ScrollbackTotal())
	}
}

func TestConvertRowZeroAllocHotPath(t *testing.T) {
	p := newTestPane(t, 80, 4)
	p.mu.Lock()
	defer p.mu.Unlock()
	// Mix of plain ASCII, palette color, and RGB color (warmed in rgbCache).
	_, _ = p.term.WriteString("plain text \x1b[31mpalette\x1b[38;2;18;52;86mtruecolor\x1b[0m end")
	src := p.term.RowCells(0)
	dst := make([]protocol.Cell, 80)
	// Warm rgbCache once.
	p.convertRow(dst, src, 0, 0, false, "", nil, nil)

	allocs := testing.AllocsPerRun(200, func() {
		p.convertRow(dst, src, 0, 0, false, "", nil, nil)
	})
	if allocs != 0 {
		t.Fatalf("convertRow allocated %.2f allocs/op, want 0", allocs)
	}
}

func TestWireAttachDeltaEndToEnd(t *testing.T) {
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
	if r := srv.request(protocol.Request{Op: "create", Name: "wire-delta", Dir: t.TempDir(), Width: 30, Height: 6}); r.Error != "" {
		t.Fatal(r.Error)
	}

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	enc, dec := json.NewEncoder(c), json.NewDecoder(c)
	if err := enc.Encode(protocol.Request{Version: protocol.Version, Op: "attach", Name: "wire-delta", Width: 30, Height: 6}); err != nil {
		t.Fatal(err)
	}
	var ack protocol.Response
	if err := dec.Decode(&ack); err != nil || ack.Error != "" {
		t.Fatalf("attach: %+v %v", ack, err)
	}

	panes := make(map[int]*protocol.TerminalFrame)
	readNext := func() *protocol.Frame {
		var r protocol.Response
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		return r.Frame
	}

	// Receive initial full frame.
	f0 := readNext()
	if f0 == nil || len(f0.Panes) != 1 || f0.Panes[0].Terminal.Delta {
		t.Fatalf("expected initial full frame, got %+v", f0)
	}
	panes[f0.Panes[0].ID] = f0.Panes[0].Terminal

	// Trigger a single-row update and verify a delta frame arrives over the wire.
	srv.mu.Lock()
	sess := srv.sessions["wire-delta"]
	srv.mu.Unlock()
	sess.mu.Lock()
	p := sess.panes[1]
	sess.mu.Unlock()

	p.apply([]byte("\x1b[4;1HWIRE_DELTA_OK"))
	sawDelta := false
	for !sawDelta {
		f := readNext()
		if f == nil || len(f.Panes) == 0 {
			continue
		}
		raw := f.Panes[0].Terminal
		resolved := protocol.ApplyTerminalDelta(panes[f.Panes[0].ID], raw)
		panes[f.Panes[0].ID] = resolved
		if strings.Contains(frameText(resolved), "WIRE_DELTA_OK") {
			if raw.Delta {
				sawDelta = true
			} else {
				t.Fatalf("expected delta frame for single-row update, got full frame")
			}
		}
	}
}
