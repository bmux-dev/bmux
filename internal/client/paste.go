package client

import (
	"bytes"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

const maxPasteBytes = protocol.MaxPasteBytes

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// rawPasteEvent is delivered in place of the bracketed-paste markers, never as
// decoded keys. Text is accepted in its entirety or Err is set and Text is empty.
// JSON's string transport cannot preserve invalid UTF-8, so reject it explicitly.
type rawPasteEvent struct {
	Text string
	Err  error
	when time.Time
}

func (e *rawPasteEvent) When() time.Time { return e.when }

// pasteParser holds only a delimiter candidate plus at most maxPasteBytes of
// payload. The first literal end marker terminates a paste (the terminal protocol
// has no way to escape that marker). Rejected pastes are drained to that boundary.
type pasteParser struct {
	candidate []byte
	payload   []byte
	pasting   bool
	rejected  bool
}

// feed returns ordinary terminal bytes or one completed/error paste event.
// An oversized paste is rejected immediately, even if its end never arrives.
func (p *pasteParser) feed(b byte) ([]byte, *rawPasteEvent) {
	marker := pasteStart
	if p.pasting {
		marker = pasteEnd
	}
	p.candidate = append(p.candidate, b)
	var normal []byte
	var event *rawPasteEvent
	for len(p.candidate) > 0 && !bytes.HasPrefix(marker, p.candidate) {
		c := p.candidate[0]
		copy(p.candidate, p.candidate[1:])
		p.candidate = p.candidate[:len(p.candidate)-1]
		if !p.pasting {
			if c == 0x08 {
				normal = append(normal, "\x1b[104;5u"...)
			} else {
				normal = append(normal, c)
			}
		} else if !p.rejected {
			if len(p.payload) == maxPasteBytes {
				p.payload = nil
				p.rejected = true
				event = &rawPasteEvent{Err: errors.New("Paste rejected: exceeds 4 MiB limit"), when: time.Now()}
			} else {
				p.payload = append(p.payload, c)
			}
		}
	}
	if bytes.Equal(p.candidate, marker) {
		p.candidate = nil
		if !p.pasting {
			p.pasting = true
		} else {
			if !p.rejected {
				event = &rawPasteEvent{when: time.Now()}
				if !utf8.Valid(p.payload) {
					event.Err = errors.New("Paste rejected: invalid UTF-8 cannot be preserved by the text transport")
				} else {
					event.Text = string(p.payload)
				}
			}
			p.payload = nil
			p.pasting = false
			p.rejected = false
		}
	}
	return normal, event
}

func (p *pasteParser) finish() ([]byte, *rawPasteEvent) {
	if p.pasting {
		var event *rawPasteEvent
		if !p.rejected {
			event = &rawPasteEvent{Err: errors.New("Paste rejected: incomplete bracketed paste"), when: time.Now()}
		}
		*p = pasteParser{}
		return nil, event
	}
	normal := bytes.ReplaceAll(p.candidate, []byte{0x08}, []byte("\x1b[104;5u"))
	*p = pasteParser{}
	return normal, nil
}

// rawPasteScreen translates markers produced by navigationReader in tcell's
// own ordered event stream. Posting an interrupt directly from the reader would
// race earlier ordinary keys still waiting for tcell interpretation.
type rawPasteScreen struct {
	tcell.Screen
	pastes <-chan *rawPasteEvent
}

func (s *rawPasteScreen) translate(event tcell.Event) tcell.Event {
	if e, ok := event.(*tcell.EventPaste); ok {
		if e.Start() {
			return nil
		}
		if e.End() {
			select {
			case paste := <-s.pastes:
				return paste
			default:
				return &rawPasteEvent{Err: errors.New("Paste rejected: missing raw payload"), when: time.Now()}
			}
		}
	}
	return event
}

func (s *rawPasteScreen) PollEvent() tcell.Event {
	for {
		event := s.Screen.PollEvent()
		if event == nil {
			return nil
		}
		if translated := s.translate(event); translated != nil {
			return translated
		}
	}
}

func (s *rawPasteScreen) ChannelEvents(events chan<- tcell.Event, quit <-chan struct{}) {
	defer close(events)
	incoming := make(chan tcell.Event)
	stop := make(chan struct{})
	defer close(stop)
	go s.Screen.ChannelEvents(incoming, stop)
	for {
		select {
		case <-quit:
			return
		case event, ok := <-incoming:
			if !ok {
				return
			}
			if translated := s.translate(event); translated != nil {
				select {
				case events <- translated:
				case <-quit:
					return
				}
			}
		}
	}
}

// deliver queues the event before its synthetic marker pair reaches tcell.
// A single-slot queue bounds accepted payloads waiting for event consumption.
func (r *navigationReader) deliver(event *rawPasteEvent) error {
	if event == nil {
		return nil
	}
	if r.pastes != nil {
		select {
		case r.pastes <- event:
		case <-r.stopped:
			return io.EOF
		}
	}
	r.pending = append(r.pending, pasteStart...)
	r.pending = append(r.pending, pasteEnd...)
	return nil
}
