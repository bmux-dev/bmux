package server

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/josephferrero/bmux/internal/protocol"
)

type pane struct {
	// mu serializes emulator state and input encoding, never PTY writes.
	// bridge owns writer cancellation; done is shell exit, stopDone is owned-job
	// cleanup. Neither completion means this pane is already removed from layout.
	mu           sync.Mutex
	info         protocol.Session
	term         *vt.Emulator
	pty          *os.File
	cmd          *exec.Cmd
	revision     uint64
	title        string
	visible      bool
	style        vt.CursorStyle
	blink        bool
	modes        map[int]bool
	done         chan struct{}
	closeOnce    sync.Once
	bridge       *inputBridge
	owner        processOwner
	stopOnce     sync.Once
	stopDone     chan struct{}
	historyTotal int
	epoch        uint64
}

func newPane(name, dir, shell string, width, height, paneID int) (*pane, error) {
	width, height = dimensions(width, height)
	s := &pane{
		info: protocol.Session{Name: name, Dir: dir, Created: time.Now(), Used: time.Now()},
		term: vt.NewEmulator(width, height), visible: true, blink: true,
		modes: make(map[int]bool), done: make(chan struct{}), stopDone: make(chan struct{}), revision: 1, epoch: 1,
	}
	s.term.SetScrollbackSize(10000)
	s.term.SetCallbacks(vt.Callbacks{
		Title:            func(t string) { s.title = t },
		CursorVisibility: func(v bool) { s.visible = v },
		// The pinned x/vt version passes Cursor.Steady here despite naming
		// this callback argument "blink". Keep the inversion regression-tested.
		CursorStyle: func(style vt.CursorStyle, steady bool) { s.style, s.blink = style, !steady },
		EnableMode:  func(m ansi.Mode) { s.modes[m.Mode()] = true },
		DisableMode: func(m ansi.Mode) { delete(s.modes, m.Mode()) },
		AltScreen:   func(bool) { s.epoch++ },
	})
	s.cmd = exec.Command(shell, "-l")
	s.cmd.Dir = dir
	s.cmd.Env = shellEnv(os.Environ(), name, paneID)
	f, err := pty.StartWithSize(s.cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		_ = s.term.InputPipe().(io.Closer).Close()
		return nil, err
	}
	s.owner = newProcessOwner(s.cmd.Process.Pid)
	if s.owner.birth == "" {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		_ = f.Close()
		_ = s.term.InputPipe().(io.Closer).Close()
		return nil, fmt.Errorf("cannot establish pane process ownership")
	}
	pollable, err := pollablePTY(f)
	if err != nil {
		s.owner.signal(unix.SIGKILL)
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		_ = f.Close()
		_ = s.term.InputPipe().(io.Closer).Close()
		return nil, fmt.Errorf("PTY cannot support cancellable IO: %w", err)
	}
	s.pty = pollable
	// Drain emulator-generated input (keys, paste and terminal query replies)
	// independently of its state lock: io.Pipe writes require an active reader.
	s.bridge = newInputBridge(s.term, s.pty)
	go s.readOutput()
	go func() {
		_ = s.cmd.Wait()
		s.mu.Lock()
		s.info.Exited = true
		s.revision++
		s.mu.Unlock()
		close(s.done)
	}()
	return s, nil
}

// BMUX_PANE mirrors tmux's TMUX_PANE: editors inside a pane can identify
// themselves back to the server, e.g. for edge-crossing navigation. Incoming
// values of these variables are stripped, never trusted from the outer shell.
func shellEnv(env []string, name string, paneID int) []string {
	result := make([]string, 0, len(env)+4)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TERM", "COLORTERM", "BMUX_SESSION", "BMUX_PANE", "TMUX", "TMUX_PANE":
			continue
		}
		result = append(result, entry)
	}
	return append(result, "TERM=xterm-256color", "COLORTERM=truecolor",
		"BMUX_SESSION="+name, "BMUX_PANE="+strconv.Itoa(paneID))
}

func dimensions(w, h int) (int, int) {
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	return max(2, min(w, protocol.MaxTerminalWidth)), max(1, min(h, protocol.MaxTerminalHeight))
}

func (s *pane) readOutput() {
	defer s.closeIO()
	buf := make([]byte, 32768)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.trackHistory(func() { _, _ = s.term.Write(buf[:n]) })
			// Fence the fast drain before another state-serialized operation
			// can classify its input as user data.
			_ = s.bridge.fenceGeneratedInput(s.term.InputPipe())
			s.revision++
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *pane) closeIO() {
	s.closeOnce.Do(func() {
		s.bridge.close(io.ErrClosedPipe)
		// Close the pipe, not Emulator.Close: its Read method reads an
		// unsynchronized closed flag. Keeping that flag immutable avoids a race.
		_ = s.term.InputPipe().(io.Closer).Close()
		_ = s.pty.Close()
	})
}

func (s *pane) stop() {
	s.stopUntil(time.Now().Add(paneShutdownBudget))
}

func (s *pane) input(r protocol.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.Exited && r.Op != "resize" {
		return fmt.Errorf("pane shell has exited")
	}
	// Conservative bounds audited against the pinned x/vt encoders.
	reserve := 0
	switch r.Op {
	case "paste":
		if len(r.Text) > protocol.MaxPasteBytes {
			return fmt.Errorf("paste too large; entire request rejected")
		}
		reserve = len(r.Text) + protocol.PasteMarkerBytes
	case "key":
		reserve = 16
	case "focus":
		reserve = 3
	case "mouse":
		reserve = 128
	}
	if reserve > 0 {
		if err := s.bridge.beginUserInput(reserve); err != nil {
			return err
		}
	}
	switch r.Op {
	case "resize":
		w, h := dimensions(r.Width, r.Height)
		if !s.info.Exited {
			if err := resizePTY(s.pty, w, h); err != nil {
				return err
			}
		}
		s.trackHistory(func() { s.term.Resize(w, h) })
	case "key":
		s.term.SendKey(uv.KeyPressEvent{Code: r.Code, Text: r.Text, Mod: uv.KeyMod(r.Mod)})
	case "paste":
		s.term.Paste(r.Text)
	case "focus":
		if r.Focused {
			s.term.Focus()
		} else {
			s.term.Blur()
		}
	case "mouse":
		m := uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseButton(r.Button), Mod: uv.KeyMod(r.Mod)}
		switch r.Kind {
		case "release":
			s.term.SendMouse(uv.MouseReleaseEvent(m))
		case "motion":
			s.term.SendMouse(uv.MouseMotionEvent(m))
		case "wheel":
			s.term.SendMouse(uv.MouseWheelEvent(m))
		default:
			s.term.SendMouse(uv.MouseClickEvent(m))
		}
	default:
		return fmt.Errorf("unknown attached operation %q", r.Op)
	}
	if reserve > 0 {
		if err := s.bridge.finishUserInput(s.term.InputPipe()); err != nil {
			return err
		}
	}
	s.revision++
	return nil
}

func rgb(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// cellColor keeps palette colors as indexes so the attached terminal renders
// them with its own theme. Entries an application redefined are sent exactly.
func (s *pane) cellColor(c color.Color) string {
	var index int
	switch c := c.(type) {
	case nil:
		return ""
	case ansi.BasicColor:
		index = int(c)
	case ansi.IndexedColor:
		index = int(c)
	default:
		return rgb(c)
	}
	palette := s.term.IndexedColor(index)
	if _, standard := palette.(ansi.IndexedColor); standard {
		return protocol.PaletteColor(index)
	}
	return rgb(palette)
}

func (s *pane) cell(c *uv.Cell) protocol.Cell {
	return protocol.Cell{Text: c.Content, Width: c.Width, FG: s.cellColor(c.Style.Fg), BG: s.cellColor(c.Style.Bg),
		Attrs: c.Style.Attrs, Underline: c.Style.Underline != 0}
}

// frame returns only the requested viewport, not the entire scrollback buffer.
// A slow client cannot block PTY output while rendering or writing to its socket.
func (s *pane) frame(offset int) (*protocol.TerminalFrame, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frameLocked(offset)
}

// Anchor adjustment and visible-row construction share one state acquisition.
// Output cannot advance history between the coordinate check and cell copy.
func (s *pane) anchoredFrame(offset, previousTotal int) (*protocol.TerminalFrame, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := max(s.historyTotal, s.term.ScrollbackLen())
	if s.term.IsAltScreen() {
		total = 0
	}
	if total < previousTotal {
		offset = 0
	} else if offset > 0 {
		offset += total - previousTotal
	}
	return s.frameLocked(offset)
}

// Caller holds pane.mu.
func (s *pane) frameLocked(offset int) (*protocol.TerminalFrame, uint64) {
	history := s.term.ScrollbackLen()
	total := max(s.historyTotal, history)
	if s.term.IsAltScreen() {
		offset = 0
		history = 0
		total = 0
	}
	offset = max(0, min(offset, history))
	p := s.term.CursorPosition()
	f := &protocol.TerminalFrame{
		Name: s.info.Name, Title: s.title, Base: total - offset, Epoch: s.epoch, HistoryStart: total - history,
		Offset: offset, History: history, CursorX: p.X, CursorY: p.Y,
		CursorVisible: s.visible && offset == 0, CursorStyle: int(s.style), CursorBlink: s.blink,
		Alt: s.term.IsAltScreen(), Exited: s.info.Exited,
		Mouse: s.modes[9] || s.modes[1000] || s.modes[1002] || s.modes[1003],
	}
	f.Rows = make([][]protocol.Cell, s.term.Height())
	for y := range f.Rows {
		f.Rows[y] = make([]protocol.Cell, s.term.Width())
		for x := range f.Rows[y] {
			var c *uv.Cell
			row := history - offset + y
			if row < history {
				c = s.term.ScrollbackCellAt(x, row)
			} else {
				c = s.term.CellAt(x, row-history)
			}
			if c != nil {
				f.Rows[y][x] = s.cell(c)
			}
		}
	}
	return f, s.revision
}

func (s *pane) preview(width, height int) *protocol.TerminalFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor := s.term.CursorPosition()
	f := &protocol.TerminalFrame{Name: s.info.Name, Title: s.title, Alt: s.term.IsAltScreen(), Exited: s.info.Exited,
		Epoch: s.epoch, CursorStyle: int(s.style), CursorBlink: s.blink}
	visible := s.visible && !s.info.Exited
	startX, startY := 0, 0
	if visible {
		startX = protocol.ViewportOrigin(cursor.X, s.term.Width(), width)
		startY = protocol.ViewportOrigin(cursor.Y, s.term.Height(), height)
	}
	width = max(0, min(width, s.term.Width()))
	height = max(0, min(height, s.term.Height()))
	f.CursorX, f.CursorY = cursor.X-startX, cursor.Y-startY
	f.CursorVisible = visible && f.CursorX >= 0 && f.CursorX < width && f.CursorY >= 0 && f.CursorY < height
	f.Base = max(s.historyTotal, s.term.ScrollbackLen()) + startY
	for y := 0; y < height; y++ {
		row := make([]protocol.Cell, width)
		for x := range row {
			if c := s.term.CellAt(startX+x, startY+y); c != nil {
				row[x] = s.cell(c)
			}
		}
		f.Rows = append(f.Rows, row)
	}
	return f
}
