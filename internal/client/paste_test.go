package client

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
	"golang.org/x/term"
)

const fidelityPaste = "A\x08B\x7fC\x1b[DZ\r\nT\tQ\n\r界🙂\x01d\x01h\x1b[200~nested"

type fragmentedPasteReader struct {
	data []byte
	size int
}

func (r *fragmentedPasteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.size, len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

func TestRawPasteReaderFragmentedFidelity(t *testing.T) {
	for _, size := range []int{1, 2, 3, 5, 7, 128, 4096} {
		pastes := make(chan *rawPasteEvent, 1)
		input := "a\x08\x7f" + string(pasteStart) + fidelityPaste + string(pasteEnd) + "z\x08\x7f"
		r := &navigationReader{reader: &fragmentedPasteReader{data: []byte(input), size: size}, pastes: pastes}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		want := "a\x1b[104;5u\x7f" + string(pasteStart) + string(pasteEnd) + "z\x1b[104;5u\x7f"
		if string(got) != want {
			t.Fatalf("fragment size %d: terminal bytes %q", size, got)
		}
		event := <-pastes
		if event.Err != nil || event.Text != fidelityPaste || event.When().IsZero() {
			t.Fatalf("fragment size %d: event %+v", size, event)
		}
	}
}

func parsePasteForTest(input []byte) ([]byte, []*rawPasteEvent, pasteParser) {
	var parser pasteParser
	var output []byte
	var events []*rawPasteEvent
	for _, b := range input {
		normal, event := parser.feed(b)
		output = append(output, normal...)
		if event != nil {
			events = append(events, event)
		}
	}
	normal, event := parser.finish()
	output = append(output, normal...)
	if event != nil {
		events = append(events, event)
	}
	return output, events, parser
}

func TestRawPasteRejectsEntireOversizedInvalidAndIncompletePayload(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"incomplete", string(pasteStart) + "partial\x1b[201", "incomplete"},
		{"invalid UTF-8", string(pasteStart) + "a\xffb" + string(pasteEnd), "invalid UTF-8"},
		{"over limit", string(pasteStart) + strings.Repeat("x", maxPasteBytes+1) + string(pasteEnd) + "z", "4 MiB"},
		{"over limit incomplete", string(pasteStart) + strings.Repeat("x", maxPasteBytes+1), "4 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normal, events, _ := parsePasteForTest([]byte(tc.input))
			if len(events) != 1 || events[0].Err == nil || !strings.Contains(events[0].Err.Error(), tc.want) || events[0].Text != "" {
				t.Fatalf("paste rejection: %+v", events)
			}
			want := ""
			if tc.name == "over limit" {
				want = "z"
			}
			if string(normal) != want {
				t.Fatalf("payload leaked as keys: %q", normal)
			}
		})
	}
}

func TestRawPasteExactLimitAndFalseDelimiterPrefixes(t *testing.T) {
	payload := strings.Repeat("x", maxPasteBytes-11) + "\x1b[201x\x1b[20!"
	if len(payload) != maxPasteBytes {
		t.Fatal("bad test fixture", len(payload))
	}
	normal, events, _ := parsePasteForTest([]byte("a" + string(pasteStart) + payload + string(pasteEnd) + "b"))
	if string(normal) != "ab" || len(events) != 1 || events[0].Err != nil || events[0].Text != payload {
		t.Fatalf("exact-limit paste not preserved: events=%d normal=%q", len(events), normal)
	}
}

func TestRawPasteIncompleteReaderDeliversErrorBeforeEOF(t *testing.T) {
	pastes := make(chan *rawPasteEvent, 1)
	r := &navigationReader{reader: strings.NewReader(string(pasteStart) + "not sent"), pastes: pastes}
	normal, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(normal, append(append([]byte{}, pasteStart...), pasteEnd...)) {
		t.Fatalf("synthetic rejected-paste markers: %q %v", normal, err)
	}
	event := <-pastes
	if event.Err == nil || event.Text != "" {
		t.Fatalf("incomplete paste accepted: %+v", event)
	}
}

func TestRawPasteBackpressureCanBeStopped(t *testing.T) {
	pastes := make(chan *rawPasteEvent, 1)
	pastes <- &rawPasteEvent{}
	stop := make(chan struct{})
	r := &navigationReader{reader: strings.NewReader(string(pasteStart) + "x" + string(pasteEnd)), pastes: pastes, stopped: stop}
	done := make(chan error, 1)
	go func() { _, err := io.ReadAll(r); done <- err }()
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stopping did not release blocked paste delivery")
	}
}

// This uses an actual raw PTY and tcell's real terminfo input parser, not
// synthetic EventKey reconstruction. All terminal/clipboard resources are owned.
type pasteTestTty struct {
	file   *os.File
	master *os.File
	once   sync.Once
}

func (t *pasteTestTty) Read(p []byte) (int, error)  { return t.file.Read(p) }
func (t *pasteTestTty) Write(p []byte) (int, error) { return len(p), nil }
func (t *pasteTestTty) Start() error {
	_, err := term.MakeRaw(int(t.file.Fd()))
	return err
}
func (t *pasteTestTty) Stop() error { return nil }
func (t *pasteTestTty) Drain() error {
	// Darwin's blocking PTY reads are not necessarily woken by Close. Supply
	// an owned wake byte; navigationTty has already canceled input consumption.
	_, err := t.master.Write([]byte{0})
	return err
}
func (t *pasteTestTty) Close() error {
	t.once.Do(func() { _ = t.file.Close() })
	return nil
}
func (t *pasteTestTty) NotifyResize(func()) {}
func (t *pasteTestTty) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 80, Height: 24}, nil
}

func TestRawPastePTYTcellOrderingAndNavigation(t *testing.T) {
	for _, polling := range []bool{false, true} {
		name := "channel"
		if polling {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			tty := &pasteTestTty{file: slave, master: master}
			defer tty.Close()
			pastes := make(chan *rawPasteEvent, 1)
			wrapped := &navigationTty{Tty: tty, input: navigationReader{reader: tty, pastes: pastes}}
			info, err := tcell.LookupTerminfo("xterm-256color")
			if err != nil {
				t.Fatal(err)
			}
			base, err := tcell.NewTerminfoScreenFromTtyTerminfo(wrapped, info)
			if err != nil {
				t.Fatal(err)
			}
			screen := &rawPasteScreen{Screen: base, pastes: pastes}
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			defer screen.Fini()
			screen.EnablePaste()
			events := make(chan tcell.Event, 16)
			quit := make(chan struct{})
			defer close(quit)
			if polling {
				go func() {
					defer close(events)
					for {
						e := screen.PollEvent()
						if e == nil {
							return
						}
						select {
						case events <- e:
						case <-quit:
							return
						}
					}
				}()
			} else {
				go screen.ChannelEvents(events, quit)
			}
			go func() {
				// Delimiter fragments cross actual PTY reads, with a delay much
				// longer than tcell's escape-prefix handling would allow.
				for _, chunk := range []string{"a\x1b[2", "00~", fidelityPaste, "\x1b[20", "1~b\x08\x7f", string(pasteStart) + "second" + string(pasteEnd) + "c\x1b"} {
					_, _ = master.Write([]byte(chunk))
					time.Sleep(60 * time.Millisecond)
				}
			}()
			var ordered []tcell.Event
			u, requests := navigationUI(t)
			deadline := time.After(3 * time.Second)
			for len(ordered) < 8 {
				select {
				case e := <-events:
					switch event := e.(type) {
					case *tcell.EventPaste:
						t.Fatalf("raw wrapper leaked legacy paste marker: %#v", event)
					case *rawPasteEvent:
						u.handleRawPaste(event)
						r := nextNavigationRequest(t, requests, "paste", "")
						if r.Text != event.Text || r.PaneID != 1 || u.prefix || u.quitting {
							t.Fatalf("raw paste changed on UI-to-RPC path: %+v", r)
						}
						ordered = append(ordered, e)
					case *tcell.EventKey:
						ordered = append(ordered, e)
					}
				case <-deadline:
					t.Fatalf("timed out after %d ordered events: %+v", len(ordered), ordered)
				}
			}
			for _, tc := range []struct {
				index int
				want  string
			}{{1, fidelityPaste}, {5, "second"}} {
				e, ok := ordered[tc.index].(*rawPasteEvent)
				if !ok || e.Err != nil || e.Text != tc.want {
					t.Fatalf("event %d: %#v", tc.index, ordered[tc.index])
				}
			}
			for _, tc := range []struct {
				index int
				want  rune
			}{{0, 'a'}, {2, 'b'}, {6, 'c'}} {
				e, ok := ordered[tc.index].(*tcell.EventKey)
				if !ok || e.Rune() != tc.want {
					t.Fatalf("event %d: %#v", tc.index, ordered[tc.index])
				}
			}
			ctrlH, ok := ordered[3].(*tcell.EventKey)
			if !ok || ctrlH.Key() != tcell.KeyCtrlH {
				t.Fatalf("outside Ctrl-h: %#v", ordered[3])
			}
			backspace, ok := ordered[4].(*tcell.EventKey)
			if !ok || (backspace.Key() != tcell.KeyBackspace && backspace.Key() != tcell.KeyBackspace2) {
				t.Fatalf("outside Backspace: %#v", ordered[4])
			}
			escape, ok := ordered[7].(*tcell.EventKey)
			if !ok || escape.Key() != tcell.KeyEscape {
				t.Fatalf("lone Escape was held by paste parser: %#v", ordered[7])
			}
		})
	}
}
