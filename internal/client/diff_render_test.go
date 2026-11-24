package client

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

// diffTrackingScreen wraps SimulationScreen and mirrors writes into a real
// tcell.CellBuffer so tests can verify Clear/Fill are never called on steady
// frames and count the exact dirty cells tcell would emit on Show().
type diffTrackingScreen struct {
	tcell.SimulationScreen
	cb             tcell.CellBuffer
	style          tcell.Style
	clears, fills  int
	lastDirtyTotal int
	lastDirtyBody  int
}

func newDiffTrackingScreen(t *testing.T, w, h int) *diffTrackingScreen {
	t.Helper()
	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sim.Fini)
	sim.SetSize(w, h)
	s := &diffTrackingScreen{SimulationScreen: sim}
	s.cb.Resize(w, h)
	return s
}

func (s *diffTrackingScreen) SetSize(w, h int) {
	s.SimulationScreen.SetSize(w, h)
	s.cb.Resize(w, h)
}

func (s *diffTrackingScreen) SetStyle(style tcell.Style) {
	s.style = style
	s.SimulationScreen.SetStyle(style)
}

func (s *diffTrackingScreen) Clear() {
	s.clears++
	s.cb.Fill(' ', s.style)
	s.SimulationScreen.Clear()
}

func (s *diffTrackingScreen) Fill(r rune, style tcell.Style) {
	s.fills++
	s.cb.Fill(r, style)
	s.SimulationScreen.Fill(r, style)
}

func (s *diffTrackingScreen) Put(x, y int, str string, style tcell.Style) (string, int) {
	s.cb.Put(x, y, str, style)
	return s.SimulationScreen.Put(x, y, str, style)
}

func (s *diffTrackingScreen) PutStr(x, y int, str string) {
	s.PutStrStyled(x, y, str, s.style)
}

func (s *diffTrackingScreen) PutStrStyled(x, y int, str string, style tcell.Style) {
	cx := x
	rem := str
	for len(rem) > 0 {
		var w int
		rem, w = s.cb.Put(cx, y, rem, style)
		if w <= 0 {
			break
		}
		cx += w
	}
	s.SimulationScreen.PutStrStyled(x, y, str, style)
}

func (s *diffTrackingScreen) Show() {
	w, h := s.cb.Size()
	total, body := 0, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if s.cb.Dirty(x, y) {
				total++
				if y >= paneOriginY && y < h-1 {
					body++
				}
			}
			s.cb.SetDirty(x, y, false)
		}
	}
	s.lastDirtyTotal = total
	s.lastDirtyBody = body
	s.SimulationScreen.Show()
}

func makeFilledPaneFrame(id, x, y, w, h int, ch byte) protocol.PaneFrame {
	rows := make([][]protocol.Cell, h)
	line := strings.Repeat(string(ch), w)
	for r := range rows {
		rows[r] = cells(line)
	}
	return protocol.PaneFrame{
		ID: id, X: x, Y: y, Width: w, Height: h,
		Terminal: &protocol.TerminalFrame{Rows: rows},
	}
}

func TestDiffRenderPreservesCleanCells(t *testing.T) {
	const w, h = 80, 24
	s := newDiffTrackingScreen(t, w, h)
	u := &ui{screen: s, name: "work", settings: config.Default(), sidebarHidden: true, welcomed: true}
	f := &protocol.Frame{
		Name:    "work",
		Focused: 1,
		Panes:   []protocol.PaneFrame{makeFilledPaneFrame(1, 0, 0, w, h-2, 'A')},
	}
	u.applyFrame(f)
	u.draw()
	if s.clears != 0 || s.fills != 0 {
		t.Fatalf("draw called Clear=%d Fill=%d, want 0", s.clears, s.fills)
	}
	if s.lastDirtyBody != w*(h-2) {
		t.Fatalf("initial body dirty cells = %d, want %d", s.lastDirtyBody, w*(h-2))
	}

	// Second identical draw must mark zero body cells dirty.
	u.draw()
	if s.clears != 0 || s.fills != 0 {
		t.Fatalf("second draw called Clear=%d Fill=%d, want 0", s.clears, s.fills)
	}
	if s.lastDirtyBody != 0 {
		t.Fatalf("identical redraw dirtied %d body cells, want 0", s.lastDirtyBody)
	}

	// Changing a single cell in the pane must dirty only that 1 cell.
	f.Panes[0].Terminal.Rows[5][10].Text = "Z"
	u.draw()
	if s.lastDirtyBody != 1 {
		t.Fatalf("single-cell edit dirtied %d body cells, want 1", s.lastDirtyBody)
	}
	if got, _, _ := s.Get(10, paneOriginY+5); got != "Z" {
		t.Fatalf("updated cell = %q, want Z", got)
	}
}

func TestDiffRenderNoGhostCellsOnTransitions(t *testing.T) {
	const w, h = 100, 24
	s := newDiffTrackingScreen(t, w, h)
	u := &ui{
		screen:   s,
		name:     "api",
		conn:     &connection{},
		settings: config.Default(),
		welcomed: true,
		sessions: []protocol.Session{
			{Name: "api", Panes: 2, Clients: 1},
			{Name: "web", Panes: 1},
		},
	}
	vpW := u.paneViewportWidth(w)
	vpH := paneViewportHeight(h)

	// Start with a short terminal (fewer rows and shorter lines than pane) to
	// ensure padded/uncovered cells also clear ghost artifacts completely.
	shortPane := func(id, x, y, pw, ph int, text string) protocol.PaneFrame {
		return protocol.PaneFrame{
			ID: id, X: x, Y: y, Width: pw, Height: ph,
			Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells(text)}},
		}
	}
	assertCleanSinglePane := func(label string, expectedRow0 string) {
		t.Helper()
		ox := u.paneOriginX(w)
		for row := paneOriginY; row < h-1; row++ {
			for col := ox; col < w; col++ {
				text, style, _ := s.Get(col, row)
				_, _, attrs := style.Decompose()
				if attrs&tcell.AttrDim != 0 {
					t.Fatalf("%s: stale dim attribute at (%d,%d) text=%q", label, col, row, text)
				}
				want := " "
				if row == paneOriginY && col-ox < len(expectedRow0) {
					want = string(expectedRow0[col-ox])
				}
				if text != want {
					t.Fatalf("%s: ghost cell at (%d,%d) = %q, want %q", label, col, row, text, want)
				}
			}
		}
	}

	single := &protocol.Frame{
		Name: "api", Focused: 1,
		Panes: []protocol.PaneFrame{shortPane(1, 0, 0, vpW, vpH, "hello")},
	}
	u.applyFrame(single)
	u.draw()
	assertCleanSinglePane("initial", "hello")

	// 1. Help modal open -> close.
	u.help = true
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Keys") {
		t.Fatal("help modal did not render")
	}
	u.help = false
	u.draw()
	assertCleanSinglePane("after help close", "hello")

	// 2. Settings page open -> close.
	u.openSettings()
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Settings") {
		t.Fatal("settings page did not render")
	}
	u.closeSettings()
	u.draw()
	assertCleanSinglePane("after settings close", "hello")

	// 3. Welcome modal open -> close.
	u.welcome = true
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Welcome to bmux") {
		t.Fatal("welcome modal did not render")
	}
	u.welcome = false
	u.draw()
	assertCleanSinglePane("after welcome close", "hello")

	// 4. Delete confirmation modal open -> close.
	u.deleteTarget = u.sessions[0]
	u.prompt = "delete"
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Delete") {
		t.Fatal("delete modal did not render")
	}
	u.prompt = ""
	u.draw()
	assertCleanSinglePane("after delete modal close", "hello")

	// 5. Notice box show -> expire.
	u.notice("Copied to system clipboard")
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Copied to system clipboard") {
		t.Fatal("notice box did not render")
	}
	u.messageUntil = time.Now().Add(-time.Second)
	u.draw()
	assertCleanSinglePane("after notice expire", "hello")

	// 6. Projects popup over attached view open -> close.
	u.projects.open = true
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Projects") {
		t.Fatal("projects popup did not render")
	}
	u.projects.open = false
	u.draw()
	assertCleanSinglePane("after projects popup close", "hello")

	// 7. Picker open (list + preview, then rename prompt) -> close back to attached.
	u.picker = true
	u.previewName = "api"
	u.draw()
	if !strings.Contains(pickerScreenText(u), "(0) + api") {
		t.Fatal("picker list did not render")
	}
	u.prompt, u.newName, u.input = "rename", "api", "renamed"
	u.draw()
	if !strings.Contains(pickerScreenText(u), "Rename session api:") {
		t.Fatal("picker rename prompt did not render")
	}
	// Ensure list/preview lines from before rename prompt were wiped clean.
	if strings.Contains(pickerScreenText(u), "(0) + api") {
		t.Fatal("picker rename prompt left ghost session list rows")
	}
	u.prompt, u.picker = "", false
	u.draw()
	assertCleanSinglePane("after picker close", "hello")

	// 8. Multi-pane -> Zoom -> Unzoom -> Pane close -> Smaller-than-viewport pane.
	leftW := vpW / 2
	rightW := vpW - leftW - 1
	multi := &protocol.Frame{
		Name: "api", Focused: 2,
		Panes: []protocol.PaneFrame{
			shortPane(1, 0, 0, leftW, vpH, "left"),
			shortPane(2, leftW+1, 0, rightW, vpH, "right"),
		},
	}
	u.applyFrame(multi)
	u.draw()
	if got, _, _ := s.Get(u.paneOriginX(w)+leftW, paneOriginY); got == " " {
		t.Fatal("multi-pane divider missing")
	}
	// Zoom to single pane covering full viewport.
	zoomed := &protocol.Frame{
		Name: "api", Focused: 2, Zoomed: true,
		Panes: []protocol.PaneFrame{shortPane(2, 0, 0, vpW, vpH, "zoomed")},
	}
	u.applyFrame(zoomed)
	u.draw()
	assertCleanSinglePane("after zoom", "zoomed")

	// Sidebar toggle (hide sidebar while pane still has smaller width -> uncovered right strip).
	u.sidebarHidden = true
	u.draw()
	assertCleanSinglePane("after sidebar hide with smaller pane", "zoomed")

	// Restore sidebar and check standalone projects mode leaves zero ghosts from attached view.
	u.sidebarHidden = false
	u.draw()
	u.conn = nil
	u.projects.open = true
	u.draw()
	if strings.Contains(pickerScreenText(u), "SESSIONS") || strings.Contains(pickerScreenText(u), "zoomed") {
		t.Fatal("standalone projects mode left ghost text from attached view")
	}
	if s.clears != 0 || s.fills != 0 {
		t.Fatalf("transitions called Clear=%d Fill=%d, want 0", s.clears, s.fills)
	}
}

func TestDirtyGatingAndBurstCoalescing(t *testing.T) {
	s := newDiffTrackingScreen(t, 80, 24)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		dec := json.NewDecoder(b)
		for {
			var r protocol.Request
			if dec.Decode(&r) != nil {
				return
			}
		}
	}()
	u := &ui{
		screen:   s,
		name:     "api",
		conn:     &connection{Conn: a, enc: json.NewEncoder(a)},
		settings: config.Default(),
		welcomed: true,
		updates:  make(chan update, 32),
		sessions: []protocol.Session{{Name: "api", Panes: 1}},
	}
	u.applyFrame(&protocol.Frame{
		Name: "api", Focused: 1, Edge: 1, MessageSeq: 1,
		Panes: []protocol.PaneFrame{makeFilledPaneFrame(1, 0, 0, 50, 22, 'x')},
	})
	u.draw()

	// Idle mouse move without buttons held must not mark dirty.
	if dirty, _ := u.handleEvent(tcell.NewEventMouse(10, 10, tcell.ButtonNone, tcell.ModNone)); dirty {
		t.Fatal("idle mouse move marked UI dirty")
	}
	// Focus change without active resize banner must not mark dirty.
	if dirty, _ := u.handleEvent(tcell.NewEventFocus(false)); dirty {
		t.Fatal("focus event without resize banner marked UI dirty")
	}
	// Focus loss with active resize banner clears it and marks dirty.
	u.resizeUntil = time.Now().Add(time.Second)
	if dirty, _ := u.handleEvent(tcell.NewEventFocus(false)); !dirty || !u.resizeUntil.IsZero() {
		t.Fatal("focus loss did not clear active resize banner cleanly")
	}
	// Unknown event does not mark dirty.
	if dirty, _ := u.handleEvent(tcell.NewEventPaste(true)); dirty {
		t.Fatal("ignored EventPaste marked UI dirty")
	}

	// Sidebar refresh with identical sessions must not mark dirty.
	if u.finishSidebarRefresh(sidebarResult{sessions: []protocol.Session{{Name: "api", Panes: 1}}}) {
		t.Fatal("unchanged sidebar refresh marked UI dirty")
	}
	if !u.finishSidebarRefresh(sidebarResult{sessions: []protocol.Session{{Name: "api", Panes: 2}}}) {
		t.Fatal("changed sidebar refresh failed to mark UI dirty")
	}

	// Tick within same second with no expired timers must not mark dirty.
	now := time.Date(2026, 11, 24, 10, 20, 30, 0, time.UTC)
	u.drawPowerline(80, now)
	u.noticeVisible = false
	u.resizeBannerVisible = false
	if u.handleTick(now.Add(100 * time.Millisecond)) {
		t.Fatal("tick in same second with no timers marked UI dirty")
	}
	// Tick on next second marks dirty because powerline clock changed.
	if !u.handleTick(now.Add(time.Second)) {
		t.Fatal("tick advancing clock second did not mark UI dirty")
	}
	// Expired visible notice marks dirty even in same second.
	u.drawPowerline(80, now)
	u.noticeVisible = true
	u.messageUntil = now
	if !u.handleTick(now) {
		t.Fatal("expired visible notice did not mark UI dirty")
	}
	u.noticeVisible = false
	// Standalone projects mode hides clock, so advancing second without timers is not dirty.
	savedConn := u.conn
	u.conn = nil
	u.projects.open = true
	if u.handleTick(now.Add(time.Second)) {
		t.Fatal("tick during standalone projects with no clock marked UI dirty")
	}
	u.projects.open = false
	u.conn = savedConn

	// Burst coalescing: queue 5 resize events and 5 frames carrying intermediate
	// clipboard, notice, message, and edge transitions.
	events := make(chan tcell.Event, 16)
	for range 5 {
		events <- tcell.NewEventResize(90, 25)
	}
	u.updates <- update{response: protocol.Response{Frame: &protocol.Frame{
		Name: "api", Focused: 1, Edge: 1, MessageSeq: 1, Clipboard: "burst-clip",
		Panes: []protocol.PaneFrame{{ID: 1, Width: 50, Height: 22, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("frame-1")}}}},
	}}}
	u.updates <- update{response: protocol.Response{Frame: &protocol.Frame{
		Name: "api", Focused: 1, Edge: 2, EdgeDirection: "left", MessageSeq: 1,
		Panes: []protocol.PaneFrame{{ID: 1, Width: 50, Height: 22, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("frame-2")}}}},
	}}}
	u.updates <- update{response: protocol.Response{Frame: &protocol.Frame{
		Name: "api", Focused: 1, Edge: 2, MessageSeq: 2, Message: "burst-msg",
		Panes: []protocol.PaneFrame{{ID: 1, Width: 50, Height: 22, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("frame-3")}}}},
	}}}
	u.updates <- update{response: protocol.Response{Frame: &protocol.Frame{
		Name: "api", Focused: 1, Edge: 2, MessageSeq: 2,
		Panes: []protocol.PaneFrame{{ID: 1, Width: 50, Height: 22, Terminal: &protocol.TerminalFrame{Rows: [][]protocol.Cell{cells("latest-frame")}}}},
	}}}

	dirty, resized, closed := u.drainBurst(events)
	if !dirty || !resized || closed {
		t.Fatalf("drainBurst = dirty:%v resized:%v closed:%v, want true,true,false", dirty, resized, closed)
	}
	if len(events) != 0 || len(u.updates) != 0 {
		t.Fatalf("queues not drained: events=%d updates=%d", len(events), len(u.updates))
	}
	if got := string(s.GetClipboardData()); got != "burst-clip" {
		t.Fatalf("intermediate clipboard lost: got %q, want burst-clip", got)
	}
	if !u.sidebarFocus {
		t.Fatal("intermediate left edge transition lost")
	}
	if u.message != "burst-msg" {
		t.Fatalf("intermediate message transition lost: got %q, want burst-msg", u.message)
	}
	if u.frame == nil || len(u.frame.Rows) == 0 || u.frame.Rows[0][0].Text != "l" {
		t.Fatalf("final frame not applied: %+v", u.frame)
	}
}

func TestAppMouseDownReleaseEmptyRowsNoPanic(t *testing.T) {
	s := focusTestScreen(t, 40, 12)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	u := &ui{
		screen:         s,
		conn:           &connection{Conn: a, enc: json.NewEncoder(a)},
		focused:        1,
		frame:          &protocol.TerminalFrame{Rows: nil, Mouse: true},
		appMouseDown:   true,
		appMouseButton: int(uv.MouseLeft),
	}
	requests := make(chan protocol.Request, 4)
	go func() {
		dec := json.NewDecoder(b)
		var r protocol.Request
		if dec.Decode(&r) == nil {
			requests <- r
		}
	}()
	// Must not panic when u.frame.Rows is empty.
	u.mouse(tcell.NewEventMouse(5, 3, tcell.ButtonNone, tcell.ModNone))
	if u.appMouseDown {
		t.Fatal("appMouseDown was not cleared on release")
	}
	select {
	case r := <-requests:
		if r.Op != "mouse" || r.Kind != "release" || r.X != 0 || r.Y != 0 {
			t.Fatalf("unexpected release request: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("expected mouse release request")
	}
}

func TestKeyRequestSpecialZeroAllocs(t *testing.T) {
	for _, key := range []tcell.Key{tcell.KeyUp, tcell.KeyEnter, tcell.KeyBacktab, tcell.KeyF1, tcell.KeyCtrlA} {
		ev := tcell.NewEventKey(key, 0, tcell.ModNone)
		allocs := testing.AllocsPerRun(100, func() {
			r := keyRequest(ev)
			if r.Code == 0 {
				t.Fatalf("key %v returned zero code", key)
			}
		})
		if allocs != 0 {
			t.Fatalf("keyRequest(%v) allocated %.1f times/op, want 0", key, allocs)
		}
	}
}

func TestDrawPaneDividersSinglePaneFastPathAndMultiPaneAllocs(t *testing.T) {
	s := focusTestScreen(t, 120, 40)
	u := &ui{screen: s, sidebarHidden: true}

	// Single pane smaller than viewport: fast-path returns without drawing floating dividers.
	single := &protocol.Frame{
		Focused: 1,
		Panes:   []protocol.PaneFrame{{ID: 1, Width: 40, Height: 20}},
	}
	u.applyFrame(single)
	u.drawPaneDividers(120, 40)
	if text, _, _ := s.Get(40, paneOriginY+5); text != " " && text != "" {
		t.Fatalf("single-pane fast path drew unexpected divider %q", text)
	}
	allocs := testing.AllocsPerRun(100, func() {
		u.drawPaneDividers(120, 40)
	})
	if allocs != 0 {
		t.Fatalf("single-pane drawPaneDividers allocated %.1f times/op, want 0", allocs)
	}

	// Multi-pane steady state reuses scratch maps with zero allocations.
	multi := &protocol.Frame{
		Focused: 1,
		Panes: []protocol.PaneFrame{
			{ID: 1, X: 0, Y: 0, Width: 59, Height: 18},
			{ID: 2, X: 60, Y: 0, Width: 60, Height: 18},
			{ID: 3, X: 0, Y: 19, Width: 59, Height: 19},
			{ID: 4, X: 60, Y: 19, Width: 60, Height: 19},
		},
	}
	u.applyFrame(multi)
	u.drawPaneDividers(120, 40) // warm up reusable scratch maps
	if text, _, _ := s.Get(59, paneOriginY+18); text != "╋" {
		t.Fatalf("multi-pane cross junction = %q, want ╋", text)
	}
	allocs = testing.AllocsPerRun(100, func() {
		u.drawPaneDividers(120, 40)
	})
	if allocs != 0 {
		t.Fatalf("steady multi-pane drawPaneDividers allocated %.1f times/op, want 0", allocs)
	}
}

func BenchmarkDrawPaneDividersSinglePane(b *testing.B) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		b.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(200, 60)
	u := &ui{screen: s, sidebarHidden: true, workspace: &protocol.Frame{
		Focused: 1,
		Panes:   []protocol.PaneFrame{{ID: 1, Width: 200, Height: 58}},
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.drawPaneDividers(200, 60)
	}
}

func BenchmarkDrawPaneDividersMultiPane(b *testing.B) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		b.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(200, 60)
	u := &ui{screen: s, sidebarHidden: true, workspace: &protocol.Frame{
		Focused: 1,
		Panes: []protocol.PaneFrame{
			{ID: 1, X: 0, Y: 0, Width: 99, Height: 28},
			{ID: 2, X: 100, Y: 0, Width: 100, Height: 28},
			{ID: 3, X: 0, Y: 29, Width: 99, Height: 29},
			{ID: 4, X: 100, Y: 29, Width: 100, Height: 29},
		},
	}}
	u.drawPaneDividers(200, 60)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.drawPaneDividers(200, 60)
	}
}

func BenchmarkKeyRequestSpecial(b *testing.B) {
	ev := tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = keyRequest(ev)
	}
}
