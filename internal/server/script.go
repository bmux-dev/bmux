package server

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Scripting operations (bmux capture, bmux send) act on one pane from
// outside any attachment: they never resize the session, move focus, count
// as viewing it or change its recent-use order.

// errStale refuses a send-input whose if_seq no longer matches the pane.
var errStale = errors.New("stale")

// paneTarget names a pane the way scripting targets do (SESSION:PANE,
// SESSION:WINDOW., SESSION:WINDOW.INDEX): a session-wide pane number, a
// window's focused pane, or a position in a window's layout. The zero value
// is the active window's focused pane.
type paneTarget struct{ window, index, pane int }

func targetOf(r protocol.Request) paneTarget {
	return paneTarget{window: r.Window, index: r.PaneIndex, pane: r.PaneID}
}

// named reports whether t picks one pane itself rather than whichever is
// focused.
func (t paneTarget) named() bool { return t.pane != 0 || t.index != 0 }

// Caller holds session.mu. resolveLocked turns t into the pane ID it names.
// The server resolves targets so a script never races a layout change
// between looking a pane up and using it.
func (s *session) resolveLocked(t paneTarget) (int, error) {
	name := s.info.Name
	switch {
	case t.pane != 0:
		if s.panes[t.pane] != nil && s.windowOfLocked(t.pane) >= 0 {
			return t.pane, nil
		}
		if t.pane <= len(s.windows) {
			// tmux's session:window; bmux numbers panes across the session.
			return 0, fmt.Errorf("no pane %d in session %s; for window %d, write %s:%d.", t.pane, name, t.pane, name, t.pane)
		}
		return 0, fmt.Errorf("no pane %d in session %s", t.pane, name)
	case t.window == 0 && t.index != 0:
		return 0, fmt.Errorf("a pane position needs a window, as in %s:1.%d", name, t.index)
	case t.window == 0:
		if id := s.focusedLocked(); id != 0 {
			return id, nil
		}
		return 0, fmt.Errorf("session %s has no panes", name)
	case t.window < 0 || t.window > len(s.windows):
		return 0, fmt.Errorf("no window %d in session %s, which has %d", t.window, name, len(s.windows))
	}
	w := s.windows[t.window-1]
	if t.index == 0 {
		return w.focused, nil
	}
	var ids []int
	w.root.leaves(&ids)
	if t.index < 0 || t.index > len(ids) {
		return 0, fmt.Errorf("window %d of session %s has %d panes, not %d", t.window, name, len(ids), t.index)
	}
	return ids[t.index-1], nil
}

// Caller holds session.mu. placeLocked is pane id's 1-based window number
// and position in that window's layout, or 0, 0.
func (s *session) placeLocked(id int) (window, index int) {
	i := s.windowOfLocked(id)
	if i < 0 {
		return 0, 0
	}
	var ids []int
	s.windows[i].root.leaves(&ids)
	for j, leaf := range ids {
		if leaf == id {
			return i + 1, j + 1
		}
	}
	return i + 1, 0
}

// targetPane resolves t on a live session.
func (s *session) targetPane(t paneTarget) (int, *pane, int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.stopping.Load() {
		return 0, nil, 0, 0, fmt.Errorf("session has been terminated")
	}
	id, err := s.resolveLocked(t)
	if err != nil {
		return 0, nil, 0, 0, err
	}
	window, index := s.placeLocked(id)
	return id, s.panes[id], window, index, nil
}

// capturePane returns the targeted pane's agent activity and visible text.
func (s *session) capturePane(t paneTarget) (protocol.PaneActivity, string, error) {
	id, p, window, index, err := s.targetPane(t)
	if err != nil {
		return protocol.PaneActivity{}, "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	a, _ := p.activityLocked(id)
	a.Window, a.Index = window, index
	return a, p.textLocked(), nil
}

// paneZoomed reports whether the window showing pane id is zoomed, which
// may be a background window. Pane 0 is the focused pane. Read-only.
func (s *session) paneZoomed(id int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.stopping.Load() {
		return false, fmt.Errorf("session has been terminated")
	}
	if id == 0 {
		id = s.focusedLocked()
	}
	if s.panes[id] == nil {
		return false, fmt.Errorf("pane not found")
	}
	i := s.windowOfLocked(id)
	if i < 0 {
		return false, fmt.Errorf("pane not found")
	}
	return s.windows[i].zoomed, nil
}

// sendInput writes key presses and pastes to one pane. It never falls back
// to the focused pane: input meant for an agent must not reach a shell, so
// t must name a pane, not only a session or window.
func (s *session) sendInput(t paneTarget, ifSeq uint64, input []protocol.Input) (protocol.PaneActivity, error) {
	if !t.named() {
		return protocol.PaneActivity{}, fmt.Errorf("send-input needs a pane")
	}
	id, p, window, index, err := s.targetPane(t)
	if err != nil {
		return protocol.PaneActivity{}, err
	}
	a, err := p.send(id, ifSeq, input)
	if a.Pane != 0 {
		a.Window, a.Index = window, index
	}
	return a, err
}

// textLocked is the visible screen as plain text: one line per row without
// trailing blanks, and no trailing empty rows. Caller holds pane.mu.
func (s *pane) textLocked() string {
	var text strings.Builder
	blankRows := 0
	for y := range s.term.Height() {
		var row strings.Builder
		for x := range s.term.Width() {
			c := s.term.CellAt(x, y)
			switch {
			case c == nil || c.Content == "" && c.Width > 0:
				row.WriteByte(' ')
			case c.Width == 0:
				// The second column of a wide character.
			default:
				row.WriteString(c.Content)
			}
		}
		line := strings.TrimRight(row.String(), " ")
		if line == "" {
			blankRows++
			continue
		}
		for ; blankRows > 0; blankRows-- {
			text.WriteByte('\n')
		}
		text.WriteString(line)
		text.WriteByte('\n')
	}
	return text.String()
}

// send writes input as one batch and returns the pane's activity. With
// ifSeq set, it writes nothing unless the agent seq still matches: the check
// and the write share one pane.mu acquisition, so no state change can land
// between them.
func (s *pane) send(id int, ifSeq uint64, input []protocol.Input) (protocol.PaneActivity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, _ := s.activityLocked(id)
	if ifSeq != 0 && ifSeq != s.agentSeq {
		return a, fmt.Errorf("%w: the agent's state changed since seq %d (now %d); capture again", errStale, ifSeq, s.agentSeq)
	}
	if s.info.Exited {
		return a, fmt.Errorf("pane shell has exited")
	}
	if len(input) == 0 {
		return a, fmt.Errorf("nothing to send")
	}
	reserve := 0
	for _, in := range input {
		if in.Paste != "" {
			reserve += len(in.Paste) + protocol.PasteMarkerBytes
		} else {
			reserve += 16
		}
	}
	if reserve > protocol.MaxInputBytes {
		return a, fmt.Errorf("input too large; entire request rejected")
	}
	s.lastInput = time.Now()
	if err := s.bridge.beginUserInput(reserve); err != nil {
		return a, err
	}
	for _, in := range input {
		if in.Paste != "" {
			s.term.Paste(in.Paste)
			continue
		}
		key := uv.KeyPressEvent{Code: in.Code, Mod: uv.KeyMod(in.Mod)}
		// Printable keys carry their text, as attached clients send them.
		if key.Mod&(uv.ModCtrl|uv.ModAlt) == 0 && in.Code < uv.KeyExtended && unicode.IsPrint(in.Code) {
			key.Text = string(in.Code)
		}
		s.term.SendKey(key)
	}
	if err := s.bridge.finishUserInput(s.term.InputPipe()); err != nil {
		return a, err
	}
	s.revision++
	return a, nil
}
