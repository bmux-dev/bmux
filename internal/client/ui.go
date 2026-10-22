// Package client provides the interactive picker and modeless terminal viewport.
package client

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
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
	drag                            dragScroll // auto-scroll while a selection drag is outside the pane
	appMouseDown                    bool
	appMouseButton                  int
	linkHeld                        bool               // a Ctrl+click opened a link; swallow the rest of that press
	opener                          func(string) error // opens links; nil means openLink
	divider                         *dividerDrag       // divider being dragged, or nil
	quitting                        bool
	clipboard                       *clipboardWorker
	clipboardSeq                    uint64
	sidebarHidden, sidebarFocus     bool
	sidebarSelected                 sidebarTarget
	sidebarBusy                     bool
	sidebarResults                  chan sidebarResult
	edge                            uint64 // last seen Frame.Edge for this attachment
	edgeKnown                       bool
	messageSeq                      uint64 // last seen Frame.MessageSeq for this attachment
	messageKnown                    bool
	alertsKnown                     bool
	sound                           func(blocked bool)
	reconnects                      chan string
	closeConfirm                    int // pane awaiting y to close, or 0
	closeWindow                     int // window number awaiting y to close, or 0
	closeCommand                    string
	renameWindow                    int         // window the rename-window prompt names
	windowTabs                      []windowTab // top-bar window labels, for clicks
	updateHintFrom                  int         // bottom-bar column of the update hint; 0 when hidden
	projects                        projectPicker
	projectResults                  chan projectResult
	view                            scrollView // the keyboard scrollback view, when open
	settings                        config.Config
	configPath                      string
	configEdge                      uint64 // last seen Frame.Config for this attachment
	configKnown                     bool
	title                           string            // window title last sent to the outer terminal
	rootKeys, prefixKeys            map[string]string // key bindings; see keyTables
	help                            bool              // key list overlay open
	helpOffset                      int               // first key list line shown
	integrations                    integrationsPanel
	settingsPage                    settingsPage
	welcome, welcomed               bool // welcome screen open; already shown by this client
}

// Run attaches to session name, or opens the session picker when name is "".
func Run(socket, name, cwd string) error {
	return run(socket, cwd, func(u *ui) error {
		if name == "" {
			u.openPicker()
			return nil
		}
		return u.switchTo(name)
	})
}

// RunProjects opens the new-session popup without attaching first; leaving it
// without choosing a project exits.
func RunProjects(socket, cwd string) error {
	return run(socket, cwd, func(u *ui) error {
		u.openProjects()
		return nil
	})
}

func run(socket, cwd string, start func(*ui) error) error {
	path, pathErr := config.Path()
	settings, configErr := config.Load(path, os.Getenv)
	if pathErr != nil {
		configErr = pathErr
	}
	screen, err := newNavigationScreen(settings.UI.TerminalPadding)
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
	u := &ui{screen: screen, socket: socket, cwd: cwd, updates: make(chan update, 32), previews: make(chan previewResult, 4), deletions: make(chan deleteResult, 1), sidebarResults: make(chan sidebarResult, 1), projectResults: make(chan projectResult, 1), reconnects: make(chan string, 1), done: make(chan struct{})}
	u.configPath = path
	u.applySettings(settings)
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
	if err := start(u); err != nil {
		return err
	}
	if configErr != nil {
		u.notice("Config: " + configErr.Error())
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
				// Before the ack check: a frame skipped for being stale still
				// carries the only copy of an app's clipboard write.
				if f.Clipboard != "" {
					u.appCopy(f.Clipboard)
				}
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
		case <-u.dragScrollC():
			u.dragScrollTick()
		case result := <-u.projectResults:
			u.finishProjects(result)
		case name := <-u.reconnects:
			// Only if nothing else was chosen while the server was away.
			if u.conn == nil && u.picker && u.prompt == "" && !u.projects.open {
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
	u.edgeKnown, u.configKnown, u.messageKnown = false, false, false
	u.sidebarFocus = false
	u.closeConfirm, u.closeWindow = 0, 0
	u.view = scrollView{}
	u.help, u.integrations.open, u.settingsPage.open, u.welcome = false, false, false, false
	u.selections = nil
	u.paneSizes = nil
	u.selection = selection{}
	u.appMouseDown = false
	u.divider = nil
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
	u.edgeKnown, u.configKnown, u.messageKnown = false, false, false
	u.sidebarFocus = false
	u.closeConfirm, u.closeWindow = 0, 0
	u.view = scrollView{}
	u.help, u.integrations.open, u.settingsPage.open, u.welcome = false, false, false, false
	u.generation++
	generation := u.generation
	u.picker, u.prefix, u.prompt = false, false, ""
	u.searching = false
	u.previewEpoch++
	u.preview = nil
	u.previewRevision = 0
	u.selection = selection{}
	u.divider = nil
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
	u.view = scrollView{} // the picker shows another session's preview
	u.searching = false
	if u.prompt == "rename-window" {
		u.prompt, u.promptError = "", "" // names the attached session's window
	}
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
	if u.projects.open {
		u.resizeUntil = time.Time{}
		u.projectsKey(e)
		return
	}
	if u.picker {
		u.resizeUntil = time.Time{}
		u.pickerKey(e)
		return
	}
	if u.closeConfirm != 0 || u.closeWindow != 0 {
		u.resizeUntil = time.Time{}
		u.answerClose(e)
		return
	}
	if u.prompt == "rename-window" {
		u.renameWindowKey(e)
		return
	}
	if u.help {
		u.helpKey(e)
		return
	}
	if u.integrations.open {
		u.integrationsKey(e)
		return
	}
	if u.settingsPage.open {
		u.settingsKey(e)
		return
	}
	if u.welcome {
		u.welcomeKey(e)
		return
	}
	if u.view.active {
		u.resizeUntil = time.Time{}
		u.viewKey(e)
		return
	}
	if u.sidebarFocus || u.prompt == "delete" {
		u.resizeUntil = time.Time{}
		u.sidebarKey(e)
		return
	}
	if !u.prefix && now.Before(u.resizeUntil) {
		if action := u.prefixAction(e); strings.HasPrefix(action, "resize-") {
			u.run(action, now)
			return
		}
	}
	// Anything other than a repeated resize key ends the repeat window, but
	// still follows its ordinary binding (or goes to the application).
	u.resizeUntil = time.Time{}
	copyKey := e.Key() == tcell.KeyCtrlC || (e.Key() == tcell.KeyRune && (e.Rune() == 'c' || e.Rune() == 'C') && e.Modifiers()&tcell.ModCtrl != 0)
	if copyKey && e.Modifiers()&tcell.ModShift != 0 && u.selection.active {
		u.copy()
		return
	}
	if u.prefix {
		u.prefix = false
		if action := u.prefixAction(e); action != "" {
			u.run(action, now)
		} else {
			u.notice(u.prefixHelp())
		}
		return
	}
	if action := u.rootAction(e); action != "" {
		u.run(action, now)
		return
	}
	if u.isPrefix(e) {
		u.prefix = true
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

func (u *ui) pickerKey(e *tcell.EventKey) {
	defer u.requestPreview()
	if u.prompt == "delete" {
		u.answerDelete(e)
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
	if u.selection.observed && u.selection.missingRows() {
		// Auto-scroll outran the frames: copy from the server's history.
		a, b := u.selection.bounds()
		r, err := Request(u.socket, protocol.Request{Op: "history-text", Name: u.name, PaneID: u.focused, Range: &protocol.TextRange{
			Epoch: u.selection.epoch, FromX: a.x, FromY: a.y, ToX: b.x, ToY: b.y, Mode: "char",
		}})
		if err != nil {
			u.notice("Could not copy: " + err.Error())
			return
		}
		u.copyText(r.Text)
		return
	}
	text := u.selection.text()
	if text == "" {
		u.notice("Select text by dragging first")
		return
	}
	u.copyText(text)
}

// copyText puts text on the clipboard of the machine running this client.
func (u *ui) copyText(text string) {
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
	if result.Seq == 0 {
		// An app's OSC 52 write, like Neovim yanking: quiet unless it fails.
		if result.Err != nil {
			u.notice("Clipboard failed for an app's copy: " + result.Err.Error())
		}
		return
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

// appCopy copies text an app in the session sent with OSC 52 through the
// same helpers as a selection, as clipboard result sequence 0.
func (u *ui) appCopy(text string) {
	if u.clipboard == nil {
		u.finishClipboard(clipboardResult{Text: text})
		return
	}
	if err := u.clipboard.Submit(0, text); err != nil {
		u.notice(err.Error())
	}
}

func (u *ui) mouse(e *tcell.EventMouse) {
	if e.Buttons() != tcell.ButtonNone {
		u.resizeUntil = time.Time{}
	}
	x, y := e.Position()
	screenY := y
	if u.projects.open {
		return // keyboard only
	}
	if u.view.active && e.Buttons()&(tcell.Button1|tcell.Button2|tcell.Button3) != 0 {
		// The mouse works as always: a click leaves the keyboard view.
		u.closeScrollView()
	}
	if u.picker {
		u.pickerMouse(e, x, y)
		return
	}
	if u.prompt == "delete" {
		// The sidebar's delete question is modal: a click keeps the session.
		if e.Buttons()&(tcell.Button1|tcell.Button2|tcell.Button3) != 0 {
			u.prompt, u.promptError = "", ""
		}
		return
	}
	if u.help || u.integrations.open || u.settingsPage.open || u.welcome {
		// Overlays are modal: a click closes the key list or panel and
		// reaches nothing else. The welcome screen waits for its keys.
		if e.Buttons()&(tcell.Button1|tcell.Button2|tcell.Button3) != 0 {
			u.help, u.integrations.open, u.settingsPage.open = false, false, false
		}
		return
	}
	if u.frame == nil {
		return
	}
	if u.linkHeld {
		if e.Buttons() == tcell.ButtonNone {
			u.linkHeld = false
		}
		return
	}
	screenWidth, screenHeight := u.screen.Size()
	if y == screenHeight-1 && e.Buttons()&tcell.Button1 != 0 && !u.selection.dragging && !u.appMouseDown && u.divider == nil {
		if hint := u.updateHint(); hint != nil && u.updateHintFrom > 0 && x >= u.updateHintFrom {
			u.notice(updateHintMessage(hint))
			return
		}
		u.openPicker()
		return
	}
	if y == 0 && e.Buttons()&tcell.Button1 != 0 && !u.selection.dragging && !u.appMouseDown && u.divider == nil {
		u.windowClick(x)
		return
	}
	// Sidebar columns are chrome. A drag or app press that started in a pane
	// still receives its motion and release there.
	if ox := u.paneOriginX(screenWidth); ox > 0 {
		if x < ox && !u.selection.dragging && !u.appMouseDown && u.divider == nil {
			if e.Buttons()&tcell.Button1 != 0 && screenY >= paneOriginY && screenY < screenHeight-1 {
				u.sidebarClick(y, screenHeight)
			}
			return
		}
		x -= ox
	}
	// Dividers are chrome too: pressing one and dragging moves it, like
	// tmux with mouse on. Focus and selection stay as they are.
	if u.divider != nil {
		if e.Buttons()&tcell.Button1 == 0 {
			u.divider = nil
		} else {
			u.dragDivider(x, y-paneOriginY)
		}
		return
	}
	if e.Buttons()&tcell.Button1 != 0 && !u.selection.dragging && !u.appMouseDown && screenY >= paneOriginY && screenY < screenHeight-1 {
		if d, ok := u.dividerAt(x, y-paneOriginY); ok {
			u.divider = &d
			return
		}
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
			u.stopDragScroll()
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
	if u.selection.dragging && e.Buttons()&tcell.Button1 != 0 && u.clickCount == 1 {
		visibleRows := min(len(u.frame.Rows), pos.Height, max(0, screenHeight-1-paneOriginY-pos.Y))
		if y < 0 || y >= visibleRows {
			u.dragOutside(x, y, visibleRows, pos.Width)
			u.selection.remember(u.frame)
			return
		}
		u.stopDragScroll()
	}
	// Chrome never routes presses/motion/wheel events into an app, including
	// while an older frame is still waiting for the resize acknowledgement.
	if screenY < paneOriginY || screenY >= screenHeight-1 {
		return
	}
	if x < 0 || x >= pos.Width || y < 0 || y >= pos.Height || y >= len(u.frame.Rows) || len(u.frame.Rows[y]) == 0 {
		return
	}
	x = max(0, min(x, len(u.frame.Rows[y])-1))
	if e.Buttons()&tcell.Button1 != 0 && e.Modifiers()&tcell.ModCtrl != 0 && !u.selection.dragging && !u.appMouseDown {
		// Ctrl+click opens a link and nothing else: no selection, and the
		// application never sees the click.
		if link := linkAt(u.frame, x, y); link != "" {
			u.linkHeld = true
			open := u.opener
			if open == nil {
				open = openLink
			}
			if err := open(link); err != nil {
				u.notice(err.Error())
			}
			return
		}
	}
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
		u.stopDragScroll()
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
	if u.projects.open && u.conn == nil && !u.picker {
		u.screen.Fill(' ', uiBaseStyle) // bmux project: nothing behind the popup
	} else if u.picker {
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
		if u.help {
			u.drawHelp(w, h)
		} else if u.integrations.open {
			u.drawIntegrations(w, h)
		} else if u.settingsPage.open {
			u.drawSettings(w, h)
		} else if u.welcome {
			u.drawWelcome(w, h)
		}
		if u.prompt == "delete" {
			u.drawDeleteModal(w, h)
		}
		u.drawPowerline(w, time.Now())
	}
	if u.projects.open {
		u.drawProjects(w, h)
	}
	root, _ := u.keyTables()
	footer := keyHints([2]string{u.prefixKey("sessions"), " sessions"}, [2]string{directionKeys(root, "navigate"), " navigate"}, [2]string{u.prefixKey("detach"), " detach"})
	if u.picker {
		footer = u.pickerFooter(w)
	}
	if u.frame != nil && !u.picker {
		if u.frame.Exited {
			footer = "Shell exited · " + keyHints([2]string{u.prefixKey("sessions"), ": sessions"}, [2]string{u.prefixKey("detach"), ": detach"})
		} else if u.frame.Offset > 0 {
			footer = fmt.Sprintf("Scrollback: %d lines up · typing returns to live output", u.frame.Offset)
		}
	}
	if u.projects.open {
		footer = u.projectsFooter()
	} else if u.prefix {
		footer = u.prefixHelp()
	} else if u.closeConfirm != 0 {
		footer = "Close the pane running " + u.closeCommand + "? y closes it · any other key keeps it"
	} else if u.closeWindow != 0 {
		footer = fmt.Sprintf("Close window %d, which runs %s? y closes it · any other key keeps it", u.closeWindow, u.closeCommand)
	} else if u.prompt == "delete" {
		footer = "Delete session: y yes · n/Esc no"
	} else if u.view.active {
		footer = "Scrollback: hjkl w b 0 $ g G move · [ ] prompts · v V Ctrl-v select · y copy · / ? search · n N next · Esc back"
	} else if u.sidebarFocus {
		footer = "Sidebar: j/k select · Enter/l open · d delete · Esc back · n new session"
	} else if u.help {
		footer = "Keys: j/k or arrows scroll · any other key closes"
	} else if u.integrations.open {
		footer = "Integrations: Enter installs · Esc closes"
	} else if u.settingsPage.open {
		footer = "Settings: ←/→ tab · j/k select · Enter or Space changes · Esc closes"
		if u.settingsPage.editing {
			footer = "Settings: type a value · Enter saves · Esc cancels"
		}
	} else if u.welcome {
		footer = "Welcome: Enter continues · Esc skips"
	} else if time.Now().Before(u.resizeUntil) {
		_, prefix := u.keyTables()
		footer = "Resizing: " + directionKeys(prefix, "resize") + " to continue · stops after 1s pause or another key"
	}
	if time.Now().Before(u.messageUntil) {
		footer = u.message
	}
	if !u.picker && !u.projects.open {
		identity := u.name + " | " + u.focusedPaneLabel()
		if len(identity) > 0 {
			footer = identity + " | " + footer
		}
	}
	if !u.picker && u.prompt == "rename-window" {
		footer = fmt.Sprintf("Rename window %d (Enter saves, empty uses the pane title, Esc cancels): %s", u.renameWindow, u.input)
		if u.promptError != "" {
			footer = fmt.Sprintf("Rename window %d (%s): %s", u.renameWindow, u.promptError, u.input)
		}
		u.screen.ShowCursor(min(w-1, 1+displayWidth(footer)), h-1)
	}
	if !u.picker && u.view.active && u.view.searching {
		direction := "up"
		if !u.view.up {
			direction = "down"
		}
		footer = fmt.Sprintf("Search %s (Enter searches, Esc cancels): %s", direction, u.view.input)
		u.screen.ShowCursor(min(w-1, 1+displayWidth(footer)), h-1)
	}
	if h > 1 {
		u.screen.PutStrStyled(0, h-1, fit(" "+footer, w), barStyle)
		u.drawUpdateHint(w, h, 1+displayWidth(footer))
	}
	u.updateTitle()
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

// tailFit keeps the end of s within width cells, so a long input shows the
// text next to its cursor.
func tailFit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	for displayWidth(s) > width {
		_, rest, _, _ := uniseg.FirstGraphemeClusterInString(s, -1)
		s = rest
	}
	return s
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
