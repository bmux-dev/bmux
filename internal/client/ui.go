// Package client provides the interactive picker and modeless terminal viewport.
package client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/rivo/uniseg"
)

type update struct {
	generation int
	response   protocol.Response
	err        error
}
type ui struct {
	screen                          tcell.Screen
	socket, cwd                     string
	conn                            *connection
	updates                         chan update
	done                            chan struct{}
	generation                      int
	frame                           *protocol.TerminalFrame
	workspace                       *protocol.Frame
	focused                         int
	selections                      map[int]selection
	paneSizes                       map[int][2]int
	serial, pending                 int
	name, previous                  string
	picker                          bool
	searching                       bool
	preview                         *protocol.Frame
	previewName, previewError       string
	previewEpoch                    int
	previewRevision                 uint64
	previewSize                     [2]int
	previewGeneration               string
	previewCancel                   context.CancelFunc
	previewBusy, previewUnsupported bool
	previews                        chan previewResult
	query                           string
	sessions                        []protocol.Session
	chosen                          int
	prompt, input, newName          string
	deleteTarget                    protocol.Session
	deletePending                   protocol.Session
	deleteBusy                      bool
	deletions                       chan deleteResult
	renameTarget                    protocol.Session
	promptError                     string
	prefix                          bool
	resizeUntil                     time.Time
	selection                       selection
	message                         string
	messageUntil                    time.Time
	lastClick                       time.Time
	clickPoint                      point
	clickCount                      int
	appMouseDown                    bool
	appMouseButton                  int
	quitting                        bool
	clipboard                       *clipboardWorker
	clipboardSeq                    uint64
	sidebarHidden, sidebarFocus     bool
	sidebarSelected                 sidebarTarget
	sidebarBusy                     bool
	sidebarResults                  chan sidebarResult
	edge                            uint64 // last seen Frame.Edge for this attachment
	edgeKnown                       bool
	alertsKnown                     bool
	sound                           func(blocked bool)
	reconnects                      chan string
}

func Run(socket, name, cwd string) error {
	screen, err := newNavigationScreen()
	if err != nil {
		return err
	}
	if err = screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
	screen.EnablePaste()
	screen.EnableFocus()
	u := &ui{screen: screen, socket: socket, cwd: cwd, updates: make(chan update, 32), previews: make(chan previewResult, 4), deletions: make(chan deleteResult, 1), sidebarResults: make(chan sidebarResult, 1), reconnects: make(chan string, 1), done: make(chan struct{})}
	u.clipboard = newClipboardWorker()
	u.sound = u.playAlert
	defer u.clipboard.Close()
	defer close(u.done)
	defer u.stopPreview()
	defer func() {
		if u.conn != nil {
			_ = u.conn.Close()
		}
	}()
	if name != "" {
		if err := u.switchTo(name); err != nil {
			return err
		}
	} else {
		u.openPicker()
	}
	events := make(chan tcell.Event, 64)
	quit := make(chan struct{})
	defer close(quit)
	go screen.ChannelEvents(events, quit)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	previewTicker := time.NewTicker(previewRefreshInterval)
	defer previewTicker.Stop()
	u.draw()
	for !u.quitting {
		select {
		case event, ok := <-events:
			if !ok {
				return nil
			}
			switch e := event.(type) {
			case *rawPasteEvent:
				u.handleRawPaste(e)
			case *tcell.EventKey:
				u.key(e)
			case *tcell.EventMouse:
				u.mouse(e)
			case *tcell.EventResize:
				w, h := screen.Size()
				u.send(protocol.Request{Op: "resize", Width: u.paneViewportWidth(w), Height: paneViewportHeight(h)})
				u.requestPreview()
				screen.Sync()
			case *tcell.EventFocus:
				if !e.Focused {
					u.resizeUntil = time.Time{}
				}
				u.send(protocol.Request{Op: "focus", Focused: e.Focused && !u.picker})
			}
		case next := <-u.updates:
			if next.generation != u.generation {
				continue
			}
			if next.err != nil {
				name := u.name
				_ = u.conn.Close()
				u.conn = nil
				u.openPicker()
				u.notice("Disconnected: " + next.err.Error())
				u.reconnect(name)
				continue
			}
			if next.response.Error != "" {
				u.notice(next.response.Error)
				if next.response.Ack >= u.pending {
					u.pending = 0
				}
			}
			if f := next.response.Frame; f != nil {
				if len(f.Panes) == 0 {
					u.endSession(f.Name)
					u.draw()
					continue
				}
				if next.response.Ack < u.pending {
					continue
				}
				u.pending = 0
				u.applyFrame(f)
			}
		case result := <-u.previews:
			u.acceptPreview(result)
		case result := <-u.deletions:
			u.finishDelete(result)
		case result := <-u.clipboard.Results:
			u.finishClipboard(result)
		case result := <-u.sidebarResults:
			u.finishSidebarRefresh(result)
		case name := <-u.reconnects:
			// Only if nothing else was chosen while the server was away.
			if u.conn == nil && u.picker && u.prompt == "" {
				if err := u.switchTo(name); err != nil {
					u.notice(err.Error())
				} else {
					u.notice("Reconnected to " + name)
				}
			}
		case <-previewTicker.C:
			if u.picker && u.prompt == "" {
				u.requestPreview()
			}
		case <-ticker.C:
			if u.picker && (u.prompt == "" || u.prompt == "delete") {
				u.refresh()
			} else if !u.picker && u.conn != nil {
				// Also while the sidebar is hidden: alert sounds need fresh state.
				u.refreshSidebar()
			}
		}
		u.draw()
	}
	return nil
}

func (u *ui) endSession(name string) {
	if u.conn != nil {
		_ = u.conn.Close()
		u.conn = nil
	}
	// The reader will report EOF next; invalidate it before opening the picker
	// so a normal session exit isn't presented as a connection failure.
	u.generation++
	u.pending = 0
	u.workspace, u.frame = nil, nil
	u.focused = 0
	u.edgeKnown = false
	u.sidebarFocus = false
	u.selections = nil
	u.paneSizes = nil
	u.selection = selection{}
	u.appMouseDown = false
	u.clickCount = 0
	u.name = ""
	if u.previous == name {
		u.previous = ""
	}
	u.openPicker()
	u.notice("Session " + name + " closed")
}

// reconnect waits for a restarted server (`bmux restart`) and reattaches to
// the session this client was showing, if it was restored. Restored sessions
// are listed as soon as the new server answers, so a missing one ends the wait.
func (u *ui) reconnect(name string) {
	if name == "" || u.reconnects == nil {
		return
	}
	socket, out, done := u.socket, u.reconnects, u.done
	u.notice("Server stopped; reconnecting to " + name + "…")
	go func() {
		deadline := time.Now().Add(reconnectTimeout)
		for time.Now().Before(deadline) {
			time.Sleep(250 * time.Millisecond)
			r, err := Request(socket, protocol.Request{Op: "list"})
			if err != nil {
				continue
			}
			for _, s := range r.Sessions {
				if s.Name == name {
					select {
					case out <- name:
					case <-done:
					}
					return
				}
			}
			return
		}
	}()
}

const reconnectTimeout = 20 * time.Second

func (u *ui) notice(text string) { u.message, u.messageUntil = text, time.Now().Add(5*time.Second) }

const prefixHelp = "Ctrl-a: | / - split · arrows focus · hjkl resize · m zoom · b sidebar · s sessions"

func (u *ui) send(r protocol.Request) {
	if r.PaneID == 0 && r.Op != "select-pane" && r.Op != "navigate-pane" && r.Op != "resize" && u.pending == 0 {
		r.PaneID = u.focused
	}
	if u.conn == nil {
		return
	}
	switch r.Op {
	case "split", "select-pane", "navigate-pane", "resize-pane", "zoom":
		u.serial++
		r.Seq, u.pending = u.serial, u.serial
	}
	if err := u.conn.send(r); err != nil {
		u.pending = 0
		u.notice("Connection: " + err.Error())
	}
}

func (u *ui) switchTo(name string) error {
	u.stopPreview()
	w, h := u.screen.Size()
	conn, err := attach(u.socket, name, u.paneViewportWidth(w), paneViewportHeight(h))
	if err != nil {
		return err
	}
	if u.conn != nil {
		_ = u.conn.Close()
	}
	if u.name != name {
		u.previous = u.name
	}
	u.conn, u.name, u.frame = conn, name, nil
	u.workspace, u.focused, u.selections = nil, 0, make(map[int]selection)
	u.paneSizes = make(map[int][2]int)
	u.pending = 0
	u.resizeUntil = time.Time{}
	u.edgeKnown = false
	u.sidebarFocus = false
	u.generation++
	generation := u.generation
	u.picker, u.prefix, u.prompt = false, false, ""
	u.searching = false
	u.previewEpoch++
	u.preview = nil
	u.previewRevision = 0
	u.selection = selection{}
	go func() {
		for {
			var response protocol.Response
			err := conn.dec.Decode(&response)
			select {
			case u.updates <- update{generation: generation, response: response, err: err}:
			case <-u.done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	u.send(protocol.Request{Op: "focus", Focused: true})
	u.refreshSidebar()
	return nil
}

func (u *ui) refresh() {
	selected := ""
	if s, ok := u.selectedSession(); ok {
		selected = s.Name
	}
	r, err := Request(u.socket, protocol.Request{Op: "list"})
	if err != nil {
		u.notice(err.Error())
		return
	}
	u.updateSessions(r.Sessions)
	for i, s := range u.filtered() {
		if s.Name == selected {
			u.chosen = i
			break
		}
	}
	u.requestPreview()
}
func (u *ui) openPicker() {
	u.stopPreview()
	u.resizeUntil = time.Time{}
	u.picker, u.query, u.chosen, u.prefix = true, "", 0, false
	u.searching = false
	u.previewEpoch++
	u.preview, u.previewName, u.previewError = nil, "", ""
	u.previewRevision = 0
	u.send(protocol.Request{Op: "focus", Focused: false})
	u.refresh()
	for i, s := range u.filtered() {
		if s.Name == u.name {
			u.chosen = i
			u.requestPreview()
			break
		}
	}
}
func (u *ui) filtered() []protocol.Session {
	type match struct {
		item  protocol.Session
		score int
	}
	var matches []match
	for _, s := range u.sessions {
		if score, ok := fuzzyScore(u.query, s.Name+" "+s.Dir); ok {
			matches = append(matches, match{s, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := make([]protocol.Session, len(matches))
	for i, m := range matches {
		result[i] = m.item
	}
	return result
}

func (u *ui) key(e *tcell.EventKey) {
	u.keyAt(e, time.Now())
}

func (u *ui) keyAt(e *tcell.EventKey, now time.Time) {
	if u.picker {
		u.resizeUntil = time.Time{}
		u.pickerKey(e)
		return
	}
	if u.sidebarFocus {
		u.resizeUntil = time.Time{}
		u.sidebarKey(e)
		return
	}
	if !u.prefix && e.Key() == tcell.KeyRune && e.Modifiers() == tcell.ModNone && now.Before(u.resizeUntil) {
		if direction := paneDirection(e.Rune()); direction != "" {
			u.resizePane(direction, now)
			return
		}
	}
	// Anything other than a repeated plain hjkl ends the repeat window, but
	// still follows its ordinary binding (or goes to the application).
	u.resizeUntil = time.Time{}
	copyKey := e.Key() == tcell.KeyCtrlC || (e.Key() == tcell.KeyRune && (e.Rune() == 'c' || e.Rune() == 'C') && e.Modifiers()&tcell.ModCtrl != 0)
	if copyKey && e.Modifiers()&tcell.ModShift != 0 && u.selection.active {
		u.copy()
		return
	}
	if u.prefix {
		u.prefix = false
		switch {
		case e.Key() == tcell.KeyCtrlA:
			u.resumeLive()
			u.send(keyRequest(e))
		case e.Key() == tcell.KeyRune:
			if direction := paneDirection(e.Rune()); direction != "" && e.Modifiers() == tcell.ModNone {
				u.resizePane(direction, now)
				return
			}
			switch e.Rune() {
			case '|':
				u.send(protocol.Request{Op: "split", Direction: "horizontal"})
			case '-':
				u.send(protocol.Request{Op: "split", Direction: "vertical"})
			case 'm':
				u.send(protocol.Request{Op: "zoom"})
			case 'o':
				if u.workspace != nil {
					for i, p := range u.workspace.Panes {
						if p.ID == u.focused {
							u.send(protocol.Request{Op: "select-pane", PaneID: u.workspace.Panes[(i+1)%len(u.workspace.Panes)].ID})
							break
						}
					}
				}
			case 's':
				u.openPicker()
			case 'd':
				u.quitting = true
			case 'y':
				u.copy()
			case 'n':
				u.openPicker()
				u.beginCreate()
			case 'p':
				if u.previous != "" {
					if err := u.switchTo(u.previous); err != nil {
						u.notice(err.Error())
					}
				}
			case 'b':
				u.toggleSidebar()
			default:
				u.notice(prefixHelp)
			}
		default:
			directions := map[tcell.Key]string{tcell.KeyLeft: "left", tcell.KeyRight: "right", tcell.KeyUp: "up", tcell.KeyDown: "down"}
			if direction := directions[e.Key()]; direction != "" {
				u.send(protocol.Request{Op: "select-pane", Direction: direction})
			}
		}
		return
	}
	if direction := navigationDirection(e); direction != "" {
		u.resumeLive()
		u.send(protocol.Request{Op: "navigate-pane", Direction: direction})
		return
	}
	if e.Key() == tcell.KeyCtrlA {
		u.prefix = true
		return
	}
	if e.Key() == tcell.KeyPgUp && e.Modifiers()&tcell.ModShift != 0 {
		u.scroll(10)
		return
	}
	if e.Key() == tcell.KeyPgDn && e.Modifiers()&tcell.ModShift != 0 {
		u.scroll(-10)
		return
	}
	if e.Key() == tcell.KeyEscape && u.selection.active {
		u.selection = selection{}
		return
	}
	u.resumeLive()
	r := keyRequest(e)
	if r.Code != 0 {
		u.send(r)
	}
}

func (u *ui) resumeLive() {
	u.selection = selection{}
	if u.frame != nil && u.frame.Offset > 0 {
		u.send(protocol.Request{Op: "scroll"})
	}
}
func (u *ui) scroll(delta int) { u.send(protocol.Request{Op: "scroll", Delta: delta}) }

func (u *ui) beginCreate() {
	u.promptError = ""
	u.prompt = "name"
	u.input = u.query
	if u.input == "" {
		u.input = filepath.Base(u.cwd)
	}
}
func (u *ui) pickerKey(e *tcell.EventKey) {
	defer u.requestPreview()
	if u.prompt == "delete" {
		if e.Key() == tcell.KeyRune && e.Rune() == 'y' && e.Modifiers() == tcell.ModNone {
			u.deleteSelectedSession()
		} else {
			u.prompt = ""
			u.promptError = ""
		}
		return
	}
	if u.prompt != "" {
		switch e.Key() {
		case tcell.KeyEscape:
			u.prompt = ""
			u.promptError = ""
		case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlH:
			u.input = dropRune(u.input)
			u.promptError = ""
		case tcell.KeyCtrlU:
			u.input = ""
			u.promptError = ""
		case tcell.KeyRune:
			u.input += string(e.Rune())
			u.promptError = ""
		case tcell.KeyEnter:
			if u.prompt == "name" {
				if err := validatePromptName(strings.TrimSpace(u.input)); err != nil {
					u.promptError = err.Error()
					return
				}
				u.newName, u.prompt, u.input = strings.TrimSpace(u.input), "directory", u.cwd
				return
			}
			if u.prompt == "rename" {
				if err := validatePromptName(strings.TrimSpace(u.input)); err != nil {
					u.promptError = err.Error()
					return
				}
				generation := ""
				if !u.renameTarget.Created.IsZero() {
					generation = protocol.SessionGeneration(u.renameTarget.Created)
				}
				_, err := Request(u.socket, protocol.Request{Op: "rename", Name: u.newName, NewName: strings.TrimSpace(u.input), Generation: generation})
				if err != nil {
					u.promptError = err.Error()
					return
				}
				u.prompt = ""
				u.refresh()
				return
			}
			dir := strings.TrimSpace(u.input)
			if dir == "~" || strings.HasPrefix(dir, "~/") {
				home, _ := os.UserHomeDir()
				dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
			}
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(u.cwd, dir)
			}
			w, h := u.screen.Size()
			_, err := Request(u.socket, protocol.Request{Op: "create", Name: u.newName, Dir: dir, Width: u.paneViewportWidth(w), Height: paneViewportHeight(h)})
			if err != nil {
				u.promptError = err.Error()
				return
			}
			if err := u.switchTo(u.newName); err != nil {
				u.notice(err.Error())
			}
		}
		return
	}
	u.listKey(e)
}
func dropRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}
func displayWidth(s string) int { return uniseg.StringWidth(s) }

func (u *ui) copy() {
	text := u.selection.text()
	if text == "" {
		u.notice("Select text by dragging first")
		return
	}
	u.clipboardSeq++
	if u.clipboard == nil {
		u.finishClipboard(clipboardResult{Seq: u.clipboardSeq, Text: text})
		return
	}
	if err := u.clipboard.Submit(u.clipboardSeq, text); err != nil {
		u.notice(err.Error())
	} else {
		u.notice("Copying…")
	}
}

func (u *ui) finishClipboard(result clipboardResult) {
	// Execute fallback writes in serialized copy order, but an old completion
	// must not replace the notice for a newer copy intent.
	if result.Err == nil && !result.Available {
		u.screen.SetClipboard([]byte(result.Text))
	}
	if result.Seq != u.clipboardSeq {
		return
	}
	if result.Err != nil {
		u.notice("Clipboard failed: " + result.Err.Error())
	} else if result.Available {
		u.notice("Copied to system clipboard")
	} else {
		u.notice("Clipboard requested via OSC 52; your terminal must allow it")
	}
}

func (u *ui) mouse(e *tcell.EventMouse) {
	if e.Buttons() != tcell.ButtonNone {
		u.resizeUntil = time.Time{}
	}
	x, y := e.Position()
	screenY := y
	if u.picker {
		u.pickerMouse(e, x, y)
		return
	}
	if u.frame == nil {
		return
	}
	screenWidth, screenHeight := u.screen.Size()
	if y == screenHeight-1 && e.Buttons()&tcell.Button1 != 0 && !u.selection.dragging && !u.appMouseDown {
		u.openPicker()
		return
	}
	// Sidebar columns are chrome. A drag or app press that started in a pane
	// still receives its motion and release there.
	if ox := u.paneOriginX(screenWidth); ox > 0 {
		if x < ox && !u.selection.dragging && !u.appMouseDown {
			if e.Buttons()&tcell.Button1 != 0 && screenY >= paneOriginY && screenY < screenHeight-1 {
				u.sidebarClick(y, screenHeight)
			}
			return
		}
		x -= ox
	}
	if u.workspace != nil && !u.selection.dragging && !u.appMouseDown && e.Buttons() != tcell.ButtonNone {
		for _, p := range u.workspace.Panes {
			if x >= p.X && x < p.X+p.Width && y-paneOriginY >= p.Y && y-paneOriginY < p.Y+p.Height {
				u.sidebarFocus = false
				if p.ID != u.focused {
					u.activatePane(p.ID)
					u.send(protocol.Request{Op: "select-pane", PaneID: p.ID})
				}
				break
			}
		}
	}
	pos := u.focusedPlacement()
	x -= pos.X
	y -= pos.Y
	// Release even outside the viewport, so selection and app mouse state
	// cannot get stuck when a drag ends on the status bar.
	if e.Buttons() == tcell.ButtonNone {
		if u.selection.dragging {
			u.selection.dragging = false
			if u.selection.start != u.selection.end || u.clickCount != 1 {
				u.copy()
			} else {
				u.selection = selection{}
			}
			return
		}
		if u.appMouseDown {
			visibleRows := min(len(u.frame.Rows), pos.Height, max(0, screenHeight-1-paneOriginY-pos.Y))
			u.send(protocol.Request{Op: "mouse", Kind: "release", Button: u.appMouseButton,
				X: max(0, min(x, len(u.frame.Rows[0])-1)), Y: max(0, min(y-paneOriginY, visibleRows-1))})
			u.appMouseDown = false
			return
		}
	}
	y -= paneOriginY
	// Chrome never routes presses/motion/wheel events into an app, including
	// while an older frame is still waiting for the resize acknowledgement.
	if screenY < paneOriginY || screenY >= screenHeight-1 {
		return
	}
	if x < 0 || x >= pos.Width || y < 0 || y >= pos.Height || y >= len(u.frame.Rows) || len(u.frame.Rows[y]) == 0 {
		return
	}
	x = max(0, min(x, len(u.frame.Rows[y])-1))
	buttons, mods := e.Buttons(), e.Modifiers()
	if u.frame.Mouse && mods&(tcell.ModShift|tcell.ModAlt) == 0 && !u.selection.dragging {
		m := protocol.Request{Op: "mouse", X: x, Y: y, Kind: "click"}
		switch {
		case buttons&tcell.WheelUp != 0:
			m.Button, m.Kind = int(uv.MouseWheelUp), "wheel"
		case buttons&tcell.WheelDown != 0:
			m.Button, m.Kind = int(uv.MouseWheelDown), "wheel"
		case buttons&tcell.Button1 != 0:
			m.Button = int(uv.MouseLeft)
			if u.appMouseDown {
				m.Kind = "motion"
			}
			u.appMouseDown = true
			u.appMouseButton = m.Button
		case buttons&tcell.Button2 != 0:
			m.Button = int(uv.MouseRight)
			if u.appMouseDown {
				m.Kind = "motion"
			}
			u.appMouseDown = true
			u.appMouseButton = m.Button
		case buttons&tcell.Button3 != 0:
			m.Button = int(uv.MouseMiddle)
			if u.appMouseDown {
				m.Kind = "motion"
			}
			u.appMouseDown = true
			u.appMouseButton = m.Button
		default:
			if !u.appMouseDown {
				return
			}
			m.Kind, m.Button = "release", int(uv.MouseLeft)
			u.appMouseDown = false
		}
		if mods&tcell.ModCtrl != 0 {
			m.Mod |= int(uv.ModCtrl)
		}
		u.send(m)
		return
	}
	if buttons&tcell.WheelUp != 0 {
		u.scroll(3)
		return
	}
	if buttons&tcell.WheelDown != 0 {
		u.scroll(-3)
		return
	}
	p := point{x, u.frame.Base + y}
	if buttons&tcell.Button1 != 0 {
		if !u.selection.dragging {
			if time.Since(u.lastClick) < 350*time.Millisecond && p == u.clickPoint {
				u.clickCount++
			} else {
				u.clickCount = 1
			}
			u.lastClick, u.clickPoint = time.Now(), p
			u.selection = selection{start: p, end: p, active: true, dragging: true}
			if u.clickCount == 2 {
				lo, hi := wordBounds(u.frame.Rows[y], x)
				u.selection.start.x, u.selection.end.x = lo, hi
			}
			if u.clickCount >= 3 {
				u.selection.start.x, u.selection.end.x = 0, len(u.frame.Rows[y])-1
				u.clickCount = 0
			}
		} else if u.clickCount == 1 {
			u.selection.end = p
		}
		u.selection.remember(u.frame)
	} else if u.selection.dragging {
		u.selection.dragging = false
		if u.selection.start != u.selection.end || u.clickCount != 1 {
			u.copy()
		} else {
			u.selection = selection{}
		}
	}
}

func (u *ui) draw() {
	w, h := u.screen.Size()
	if w <= 0 || h <= 0 {
		return
	}
	u.screen.Clear()
	u.screen.HideCursor()
	if u.picker {
		u.screen.Fill(' ', uiBaseStyle)
		u.drawPicker(w, h)
	} else {
		if u.workspace != nil {
			for _, p := range u.workspace.Panes {
				u.drawPane(p, w, h)
			}
		} else if u.frame != nil {
			u.drawPane(u.focusedPlacement(), w, h)
		}
		u.drawPaneDividers(w, h)
		u.drawSidebar(w, h)
		if u.sidebarFocus {
			u.screen.HideCursor()
		}
		u.drawPowerline(w, time.Now())
	}
	footer := "Ctrl-a s sessions · Ctrl-h/j/k/l navigate · Ctrl-a d detach"
	if u.picker {
		footer = u.pickerFooter(w)
	}
	if u.frame != nil && !u.picker {
		if u.frame.Exited {
			footer = "Shell exited · Ctrl-a s: sessions · Ctrl-a d: detach"
		} else if u.frame.Offset > 0 {
			footer = fmt.Sprintf("Scrollback: %d lines up · typing returns to live output", u.frame.Offset)
		}
	}
	if u.prefix {
		footer = prefixHelp
	} else if u.sidebarFocus {
		footer = "Sidebar: j/k select · Enter/l open · Esc back · n new session"
	} else if time.Now().Before(u.resizeUntil) {
		footer = "Resizing: hjkl to continue · stops after 1s pause or another key"
	}
	if time.Now().Before(u.messageUntil) {
		footer = u.message
	}
	if !u.picker {
		identity := u.name + " | " + u.focusedPaneLabel()
		if len(identity) > 0 {
			footer = identity + " | " + footer
		}
	}
	if h > 1 {
		u.screen.PutStrStyled(0, h-1, fit(" "+footer, w), barStyle)
	}
	u.screen.Show()
}

func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var result strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(s)
	for graphemes.Next() {
		cluster := strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return ' '
			}
			return r
		}, graphemes.Str())
		cw := displayWidth(cluster)
		if used+cw > width {
			break
		}
		result.WriteString(cluster)
		used += cw
	}
	result.WriteString(strings.Repeat(" ", max(0, width-used)))
	return result.String()
}

func cellColor(name string) tcell.Color {
	if index, err := strconv.Atoi(name); err == nil && index >= 0 && index <= 255 {
		return tcell.PaletteColor(index)
	}
	return tcell.GetColor(name)
}

func cellStyle(c protocol.Cell) tcell.Style {
	s := tcell.StyleDefault
	if c.FG != "" {
		s = s.Foreground(cellColor(c.FG))
	}
	if c.BG != "" {
		s = s.Background(cellColor(c.BG))
	}
	s = s.Bold(c.Attrs&uv.AttrBold != 0).Dim(c.Attrs&uv.AttrFaint != 0).Italic(c.Attrs&uv.AttrItalic != 0).
		Blink(c.Attrs&uv.AttrBlink != 0).Reverse(c.Attrs&uv.AttrReverse != 0).StrikeThrough(c.Attrs&uv.AttrStrikethrough != 0).Underline(c.Underline)
	return s
}
