package client

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
)

// tcell's legacy input parser maps both ASCII BS (Ctrl-h, 0x08) and DEL
// (the usual Backspace, 0x7f) to the same event. Normalize BS to an explicit
// CSI-u Ctrl-h event before parsing, keeping DEL as ordinary Backspace.
// Raw bracketed paste is extracted before this normalization or tcell parsing.
type navigationReader struct {
	reader  io.Reader
	pending []byte
	err     error
	buffer  [4096]byte
	input   []byte
	parser  pasteParser
	pastes  chan *rawPasteEvent
	stopped <-chan struct{}
}

func (r *navigationReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.pending) == 0 {
		if len(r.input) == 0 {
			if r.err != nil {
				normal, event := r.parser.finish()
				r.pending = append(r.pending, normal...)
				if err := r.deliver(event); err != nil {
					return 0, err
				}
				if len(r.pending) == 0 {
					err := r.err
					r.err = nil
					return 0, err
				}
				break
			}
			var n int
			var err error
			if terminal, ok := r.reader.(*terminalInput); ok && !r.parser.pasting && len(r.parser.candidate) == 1 {
				// A lone Escape is ambiguous with the start of a paste marker.
				// Match terminal escape timing; CSI prefixes and paste payloads
				// have no timeout, so their fragmented bytes stay intact.
				n, err = terminal.read(r.buffer[:], 50*time.Millisecond)
				if err == errEscapeTimeout {
					r.pending, _ = r.parser.finish()
					break
				}
			} else {
				n, err = r.reader.Read(r.buffer[:])
			}
			r.input = r.buffer[:n]
			r.err = err
			if n == 0 && err == nil {
				return 0, nil
			}
			continue
		}
		normal, event := r.parser.feed(r.input[0])
		r.input = r.input[1:]
		r.pending = append(r.pending, normal...)
		if err := r.deliver(event); err != nil {
			return 0, err
		}
	}
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		return n, nil
	}
	err := r.err
	r.err = nil
	return 0, err
}

type navigationTty struct {
	tcell.Tty
	input  navigationReader
	output sync.Mutex
	stopMu sync.Mutex
	stop   chan struct{}
	pump   *terminalInput
}

func (t *navigationTty) Write(p []byte) (int, error) {
	t.output.Lock()
	defer t.output.Unlock()
	return t.Tty.Write(p)
}

func (t *navigationTty) Read(p []byte) (int, error) { return t.input.Read(p) }
func (t *navigationTty) Start() error {
	t.stopMu.Lock()
	t.stop = make(chan struct{})
	t.input.stopped = t.stop
	t.stopMu.Unlock()
	t.input.pending = nil
	t.input.err = nil
	t.input.input = nil
	t.input.parser = pasteParser{}
	if t.input.pastes != nil {
		for len(t.input.pastes) > 0 {
			<-t.input.pastes
		}
	}
	if err := t.Tty.Start(); err != nil {
		return err
	}
	t.pump = newTerminalInput(t.Tty, t.input.stopped)
	t.input.reader = t.pump
	return nil
}

func (t *navigationTty) Drain() error {
	t.stopMu.Lock()
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
	}
	t.stopMu.Unlock()
	err := t.Tty.Drain()
	if t.pump != nil {
		<-t.pump.done
	}
	return err
}

// newNavigationScreen opens the terminal. graphics is ui.graphics: unless it
// is "off", the terminal is probed for image support before the screen starts.
func newNavigationScreen(preservePadding bool, graphics string) (tcell.Screen, error) {
	info, err := tcell.LookupTerminfo(os.Getenv("TERM"))
	if err != nil {
		return nil, err
	}
	tty, err := tcell.NewDevTty()
	if err != nil {
		return nil, err
	}
	pastes := make(chan *rawPasteEvent, 1)
	wrapped := &navigationTty{Tty: tty, input: navigationReader{reader: tty, pastes: pastes}}
	probe := probeTerminal(wrapped, graphics)
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(wrapped, screenTerminfo(info, preservePadding, graphics == "on" || probe.kitty))
	if err != nil {
		_ = tty.Close()
	}
	if err != nil {
		return nil, err
	}
	env := graphicsEnv{probe: probe, cellPixels: func() (int, int) { return cellPixels(wrapped, probe) },
		write: func(p []byte) error { _, err := wrapped.Write(p); return err }}
	return &rawPasteScreen{Screen: screen, pastes: pastes, graphics: env}, nil
}

// cellPixels is the size of one cell in pixels: from the terminal's reported
// window size when it has one, else from the startup probe, else 0, 0.
func cellPixels(tty tcell.Tty, probe terminalProbe) (width, height int) {
	if size, err := tty.WindowSize(); err == nil && size.Width > 0 && size.Height > 0 {
		if w, h := size.CellDimensions(); w > 0 && h > 0 && w <= maxCellPixels && h <= maxCellPixels {
			return w, h
		}
	}
	return probe.cellWidth, probe.cellHeight
}

func screenTerminfo(original *terminfo.Terminfo, preservePadding, exactColor bool) *terminfo.Terminfo {
	// Terminfo entries are globally cached. Clone rather than mutating them.
	// Emulators don't need millisecond hardware delays for each cell update.
	info := *original
	if !preservePadding {
		info.PadChar = ""
	}
	if exactColor {
		// Image placeholder cells carry the image id in their foreground
		// color; tcell would approximate it with the palette without these.
		if info.SetFgRGB == "" {
			info.SetFgRGB = "\x1b[38;2;%p1%d;%p2%d;%p3%dm"
		}
		if info.SetBgRGB == "" {
			info.SetBgRGB = "\x1b[48;2;%p1%d;%p2%d;%p3%dm"
		}
	}
	return &info
}
