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

// capturePane returns a pane's agent activity and visible text. Pane 0 is
// the focused pane.
func (s *session) capturePane(id int) (protocol.PaneActivity, string, error) {
	if id == 0 {
		s.mu.Lock()
		id = s.focused
		s.mu.Unlock()
	}
	p, err := s.livePane(id)
	if err != nil {
		return protocol.PaneActivity{}, "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	a, _ := p.activityLocked(id)
	return a, p.textLocked(), nil
}

// sendInput writes key presses and pastes to one pane. It never falls back
// to the focused pane: input meant for an agent must not reach a shell.
func (s *session) sendInput(id int, ifSeq uint64, input []protocol.Input) (protocol.PaneActivity, error) {
	if id == 0 {
		return protocol.PaneActivity{}, fmt.Errorf("send-input needs a pane")
	}
	p, err := s.livePane(id)
	if err != nil {
		return protocol.PaneActivity{}, err
	}
	return p.send(id, ifSeq, input)
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
