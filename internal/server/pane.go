package server

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/josephferrero/bmux/internal/graphics"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/vt"
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
	background   string // OSC 11 pane background; "" uses the attached terminal's
	paletteSeq   uint64 // incremented on OSC 4/104 palette redefinitions
	rgbCache     map[uint32]string
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
	agent        string      // self-reported label; "" when no agent reported
	agentState   string      // protocol.Agent* or ""
	attention    bool        // unseen bell, notification, block or finished turn
	agentSeq     uint64      // agentSeqs value of the latest agent or state change
	viewed       atomic.Bool // set under session.mu; read by emulator callbacks
	// Agent sampler state; see activity.go.
	detected                    string // foreground agent, "" when none
	busy, outputSeen            bool
	activeSamples, quietSamples int
	lastInput                   time.Time
	resume                      []string // reported resume command, or nil
	clipboard                   string   // latest OSC 52 write, for attached clients
	clipboardSeq                uint64   // its clipboardWrites number, 0 if none
	graphics                    *graphics.Pane
	graphicsOK                  atomic.Bool // some attached client can display images
	// agentHook runs the agent-blocked and agent-idle hooks; set by the
	// session that owns the pane. Called under pane.mu.
	agentHook func(state string)
	pipe      *panePipe // bmux pipe-pane's command; see pipe.go
}

func newPane(sessionID uint64, name, dir, shell string, width, height, paneID int) (*pane, error) {
	width, height = dimensions(width, height)
	s := &pane{
		info: protocol.Session{Name: name, Dir: dir, Created: time.Now(), Used: time.Now()},
		term: vt.NewEmulator(width, height), visible: true, blink: true,
		modes: make(map[int]bool), done: make(chan struct{}), stopDone: make(chan struct{}), revision: 1, epoch: 1,
	}
	s.term.SetScrollbackSize(currentSettings().scrollback)
	s.applyDefaultColors()
	// Make DECRQM report synchronized updates as supported (reset), not unknown.
	_, _ = s.term.WriteString(syncEnd)
	s.term.SetCallbacks(vt.Callbacks{
		Title:            func(t string) { s.title = t },
		CursorVisibility: func(v bool) { s.visible = v },
		// The pinned x/vt version passes Cursor.Steady here despite naming
		// this callback argument "blink". Keep the inversion regression-tested.
		CursorStyle: func(style vt.CursorStyle, steady bool) { s.style, s.blink = style, !steady },
		EnableMode: func(m ansi.Mode) {
			s.modes[m.Mode()] = true
			// Like tmux, an app that turns on focus events while its pane is
			// viewed hears so at once instead of at the next change.
			if m == ansi.ModeFocusEvent && s.viewed.Load() {
				s.term.Focus()
			}
		},
		DisableMode: func(m ansi.Mode) { delete(s.modes, m.Mode()) },
		AltScreen:   func(bool) { s.epoch++ },
		Bell:        s.noteAttentionLocked,
	})
	s.trackBackground()
	s.trackNotifications()
	s.trackPrompts()
	s.trackClipboard()
	s.trackGraphics(images)
	s.cmd = exec.Command(shell, "-l")
	s.cmd.Dir = dir
	s.cmd.Env = shellEnv(os.Environ(), sessionID, name, paneID)
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
		notifyPaneDirty(s)
		close(s.done)
	}()
	return s, nil
}

// BMUX_PANE mirrors tmux's TMUX_PANE: editors inside a pane can identify
// themselves back to the server, e.g. for edge-crossing navigation. Incoming
// values of these variables are stripped, never trusted from the outer shell.
// BMUX_SESSION is the name when the pane started; BMUX_SESSION_ID survives renames.
func shellEnv(env []string, sessionID uint64, name string, paneID int) []string {
	result := make([]string, 0, len(env)+4)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TERM", "COLORTERM", "BMUX_SESSION", "BMUX_SESSION_ID", "BMUX_PANE", "TMUX", "TMUX_PANE":
			continue
		}
		result = append(result, entry)
	}
	return append(result, "TERM=xterm-256color", "COLORTERM=truecolor",
		"BMUX_SESSION="+name, "BMUX_SESSION_ID="+strconv.FormatUint(sessionID, 10), "BMUX_PANE="+strconv.Itoa(paneID))
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
	var sync syncOutput
	var lastDeadline time.Time
	for {
		if deadline := sync.deadline(); !deadline.Equal(lastDeadline) {
			_ = s.pty.SetReadDeadline(deadline)
			lastDeadline = deadline
		}
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.apply(sync.feed(buf[:n], time.Now()))
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			s.apply(sync.flush())
			continue
		}
		if err != nil {
			s.apply(sync.flush())
			return
		}
	}
}

func (s *pane) apply(output []byte) {
	if len(output) == 0 {
		return
	}
	s.mu.Lock()
	s.trackHistory(func() { _, _ = s.term.Write(output) })
	if s.pipe != nil {
		s.pipe.write(output)
	}
	// Mark potential emulator replies so the next user-input admission fences
	// the drain before changing classification, avoiding a pipe round-trip on
	// every PTY output chunk.
	s.bridge.markGeneratedInput()
	s.revision++
	s.noteOutputLocked(time.Now())
	s.mu.Unlock()
	notifyPaneDirty(s)
}

func (s *pane) closeIO() {
	s.closeOnce.Do(func() {
		s.bridge.close(io.ErrClosedPipe)
		// Close the pipe, not Emulator.Close: its Read method reads an
		// unsynchronized closed flag. Keeping that flag immutable avoids a race.
		_ = s.term.InputPipe().(io.Closer).Close()
		_ = s.pty.Close()
		s.graphics.Close()
	})
}

func (s *pane) stop() {
	s.stopUntil(time.Now().Add(paneShutdownBudget))
}

// currentDir is where a pane split from this one starts, like tmux's
// #{pane_current_path}: the foreground job's directory, else the shell's.
// It queries the OS, so callers hold no state lock. "" when neither is
// readable or the directory no longer exists.
func (s *pane) currentDir() string {
	pids := make([]int, 0, 2)
	if group, err := foregroundPTYGroup(s.pty); err == nil && group > 0 {
		pids = append(pids, group)
	}
	pids = append(pids, s.cmd.Process.Pid)
	for _, pid := range pids {
		dir, err := processCwd(pid)
		if err != nil || dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func (s *pane) input(r protocol.Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.info.Exited && r.Op != "resize" {
		return fmt.Errorf("pane shell has exited")
	}
	s.lastInput = time.Now()
	// Conservative bounds audited against the pinned x/vt encoders.
	reserve := 0
	switch r.Op {
	case "paste", "type":
		if len(r.Text) > protocol.MaxPasteBytes {
			return fmt.Errorf("paste too large; entire request rejected")
		}
		reserve = len(r.Text) + protocol.PasteMarkerBytes
	case "key":
		reserve = 32 // longest: CSI code;mods;text u with 7-digit runes
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
			cw, ch := s.term.CellPixels()
			if err := resizePTY(s.pty, w, h, cw, ch); err != nil {
				return err
			}
		}
		reflowed := w != s.term.Width()
		s.trackHistory(func() { s.term.Resize(w, h) })
		if reflowed {
			// Reflow rewrote history: rows no longer sit where their IDs
			// said, so start a new coordinate epoch. Height-only changes
			// move rows into history in order, which the IDs already follow.
			s.epoch++
			s.historyTotal = s.term.ScrollbackLen()
		}
	case "key":
		s.term.SendKey(uv.KeyPressEvent{Code: r.Code, Text: r.Text, Mod: uv.KeyMod(r.Mod)})
	case "paste":
		s.term.Paste(r.Text)
	case "type":
		// Typed ahead regardless of bracketed-paste mode, so a shell runs it
		// as a command; used to resume agents in restored panes.
		s.term.SendText(r.Text)
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
	if r.Op == "resize" {
		s.revision++
	}
	return nil
}

const hexDigits = "0123456789abcdef"

var asciiStrings = func() [128]string {
	var t [128]string
	for i := range t {
		t[i] = string(byte(i))
	}
	return t
}()

func formatHexRGB(r, g, b uint8) string {
	var buf [7]byte
	buf[0] = '#'
	buf[1] = hexDigits[r>>4]
	buf[2] = hexDigits[r&0x0f]
	buf[3] = hexDigits[g>>4]
	buf[4] = hexDigits[g&0x0f]
	buf[5] = hexDigits[b>>4]
	buf[6] = hexDigits[b&0x0f]
	return string(buf[:])
}

func rgb(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return formatHexRGB(uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

func (s *pane) hexRGB(r, g, b uint8) string {
	key := uint32(r)<<16 | uint32(g)<<8 | uint32(b)
	if str, ok := s.rgbCache[key]; ok {
		return str
	}
	str := formatHexRGB(r, g, b)
	if s.rgbCache == nil {
		s.rgbCache = make(map[uint32]string, 32)
	} else if len(s.rgbCache) >= 4096 {
		clear(s.rgbCache)
	}
	s.rgbCache[key] = str
	return str
}

func (s *pane) rgbColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return s.hexRGB(uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

// vtColor keeps palette colors as indexes so the attached terminal renders
// them with its own theme. Entries an application redefined are sent exactly.
func (s *pane) vtColor(c vt.Color) string {
	if c.IsDefault() {
		return ""
	}
	if c.IsIndexed() {
		index := int(c.Index())
		palette := s.term.IndexedColor(index)
		if _, standard := palette.(ansi.IndexedColor); standard {
			return protocol.PaletteColor(index)
		}
		return s.rgbColor(palette)
	}
	r, g, b := c.RGB()
	return s.hexRGB(r, g, b)
}

// trackBackground records the background an application sets with OSC 11 and
// clears it on OSC 111, and tracks OSC 4/104 palette redefinitions so delta
// frames refresh when indexed colors change. The emulator's own BackgroundColor
// cannot tell "never set" from black, and the attached terminal never sees the
// OSC itself, so without this an app's background (opencode, pi themes) is
// silently dropped. Handlers return false so the emulator still updates its
// state and answers queries.
func (s *pane) trackBackground() {
	s.term.RegisterOscHandler(11, func(data []byte) bool {
		_, arg, _ := strings.Cut(string(data), ";")
		switch {
		case arg == "":
			s.background = ""
		case arg != "?":
			if c := ansi.XParseColor(arg); c != nil {
				s.background = rgb(c)
			}
		}
		return false
	})
	s.term.RegisterOscHandler(111, func([]byte) bool {
		s.background = ""
		return false
	})
	s.term.RegisterOscHandler(4, func([]byte) bool {
		s.paletteSeq++
		return false
	})
	s.term.RegisterOscHandler(104, func([]byte) bool {
		s.paletteSeq++
		return false
	})
}

// defaultBG is the pane background an application set with OSC 11, or "" so
// the attached terminal's own background shows through.
func (s *pane) defaultBG() string {
	return s.background
}

// convertRow converts len(dst) cells starting at column startX from src into
// dst using compact vt.Cell getters with zero per-cell allocations for ASCII,
// default/palette, and cached RGB styles. Caller holds pane.mu.
func (s *pane) convertRow(dst []protocol.Cell, src []vt.Cell, startX, srcY int, fromScrollback bool, bg string, links *[]string, placed *frameImages) {
	var (
		hasLastStyle  bool
		lastStyleID   uint32
		lastFG        string
		lastBG        string
		lastAttrs     uint8
		lastUnderline bool
		lastFgColor   vt.Color
	)
	for i := range dst {
		x := startX + i
		if x < 0 || x >= len(src) {
			dst[i] = protocol.Cell{BG: bg}
			continue
		}
		c := src[x]
		if c.IsZero() {
			dst[i] = protocol.Cell{BG: bg}
			continue
		}
		var text string
		if c.HasGrapheme() {
			if fromScrollback {
				text = s.term.ScrollbackCellGrapheme(x, srcY)
			} else {
				text = s.term.CellGrapheme(x, srcY)
			}
		}
		if text == "" {
			if r := c.Rune; r > 0 {
				if r < 128 {
					text = asciiStrings[r]
				} else if r == vt.PlaceholderRune {
					text = vt.Placeholder
				} else {
					text = string(r)
				}
			}
		}
		var out protocol.Cell
		var fgColor vt.Color
		if c.StyleID == 0 {
			out = protocol.Cell{Text: text, Width: c.Width(), BG: bg}
		} else {
			if !hasLastStyle || c.StyleID != lastStyleID {
				st := s.term.Style(c.StyleID)
				lastStyleID = c.StyleID
				hasLastStyle = true
				lastFgColor = st.Fg
				lastFG = s.vtColor(st.Fg)
				lastBG = s.vtColor(st.Bg)
				if lastBG == "" {
					lastBG = bg
				}
				lastAttrs = st.Attrs
				lastUnderline = st.Underline != 0
			}
			fgColor = lastFgColor
			out = protocol.Cell{
				Text:      text,
				Width:     c.Width(),
				FG:        lastFG,
				BG:        lastBG,
				Attrs:     lastAttrs,
				Underline: lastUnderline,
			}
		}
		if links != nil && c.HasLink() {
			var link uv.Link
			if fromScrollback {
				link = s.term.ScrollbackCellLink(x, srcY)
			} else {
				link = s.term.CellLink(x, srcY)
			}
			if link.URL != "" {
				out.Link = linkIndex(links, link.URL)
			}
		}
		if c.IsPlaceholder() {
			out = placed.remapCompact(out, fgColor, s.hexRGB)
		}
		dst[i] = out
	}
}

// linkIndex returns 1 + the position of url in links, appending it when new.
// A screen holds few distinct links, so a scan beats a map.
func linkIndex(links *[]string, url string) int {
	for i, l := range *links {
		if l == url {
			return i + 1
		}
	}
	*links = append(*links, url)
	return len(*links)
}

// paneDeltaState tracks the last TerminalFrame state sent for one pane on a
// single client attachment, enabling incremental ScrollUp / RowDelta frames
// and Unchanged short-circuits.
type paneDeltaState struct {
	valid         bool
	width, height int
	epoch         uint64
	alt           bool
	offset        int
	history       int
	base          int
	historyStart  int
	scrollCount   uint64
	paneRevision  uint64
	background    string
	paletteSeq    uint64
	name          string
	title         string
	cursorX       int
	cursorY       int
	cursorVisible bool
	cursorStyle   int
	cursorBlink   bool
	mouse         bool
	exited        bool
	rowRevs       []uint64
	links         []string
	hadImages     bool
	lastPlaced    map[uint32]graphics.Info
}

func (st *paneDeltaState) record(
	w, h int, epoch uint64, alt bool, offset, history, base, historyStart int,
	scrollCount, paneRevision uint64, background string, paletteSeq uint64,
	name, title string, cursorX, cursorY int, cursorVisible bool, cursorStyle int,
	cursorBlink, mouse, exited bool, term *vt.Emulator, links []string, placed *frameImages,
) {
	st.valid = true
	st.width, st.height = w, h
	st.epoch, st.alt = epoch, alt
	st.offset, st.history, st.base, st.historyStart = offset, history, base, historyStart
	st.scrollCount, st.paneRevision = scrollCount, paneRevision
	st.background, st.paletteSeq = background, paletteSeq
	st.name, st.title = name, title
	st.cursorX, st.cursorY = cursorX, cursorY
	st.cursorVisible, st.cursorStyle, st.cursorBlink = cursorVisible, cursorStyle, cursorBlink
	st.mouse, st.exited = mouse, exited
	if cap(st.rowRevs) < h {
		st.rowRevs = make([]uint64, h)
	} else {
		st.rowRevs = st.rowRevs[:h]
	}
	for y := range h {
		st.rowRevs[y] = term.RowRev(y)
	}
	if len(links) == 0 {
		st.links = nil
	} else {
		st.links = links
	}
	if placed != nil {
		st.hadImages = true
		st.lastPlaced = clonePlaced(placed.byApp)
	} else {
		st.hadImages = false
		st.lastPlaced = nil
	}
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
	return s.anchoredDeltaFrame(offset, previousTotal, nil)
}

func (s *pane) anchoredDeltaFrame(offset, previousTotal int, state *paneDeltaState) (*protocol.TerminalFrame, uint64) {
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
	return s.frameDeltaLocked(offset, state)
}

// Caller holds pane.mu.
func (s *pane) frameLocked(offset int) (*protocol.TerminalFrame, uint64) {
	return s.frameDeltaLocked(offset, nil)
}

// Caller holds pane.mu. When state is non-nil, emits an incremental delta or
// Unchanged marker whenever possible and updates state to match the sent frame.
func (s *pane) frameDeltaLocked(offset int, state *paneDeltaState) (*protocol.TerminalFrame, uint64) {
	history := s.term.ScrollbackLen()
	total := max(s.historyTotal, history)
	alt := s.term.IsAltScreen()
	if alt {
		offset = 0
		history = 0
		total = 0
	}
	offset = max(0, min(offset, history))
	base := total - offset
	historyStart := total - history
	w, h := s.term.Width(), s.term.Height()
	p := s.term.CursorPosition()
	cursorVisible := s.visible && offset == 0
	cursorStyle := int(s.style)
	cursorBlink := s.blink
	mouse := s.modes[9] || s.modes[1000] || s.modes[1002] || s.modes[1003]
	bg := s.defaultBG()
	placed := s.frameImages()
	curScroll := s.term.ScrollCount()

	if state != nil && state.valid &&
		state.width == w && state.height == h &&
		state.epoch == s.epoch && state.alt == alt &&
		state.background == bg && state.paletteSeq == s.paletteSeq {
		metaSame := state.name == s.info.Name &&
			state.title == s.title &&
			state.base == base &&
			state.offset == offset &&
			state.history == history &&
			state.historyStart == historyStart &&
			state.cursorX == p.X && state.cursorY == p.Y &&
			state.cursorVisible == cursorVisible &&
			state.cursorStyle == cursorStyle &&
			state.cursorBlink == cursorBlink &&
			state.mouse == mouse &&
			state.exited == s.info.Exited
		imagesUnchanged := (placed == nil && !state.hadImages) ||
			(placed != nil && state.hadImages && placedEqual(placed.byApp, state.lastPlaced))

		if offset > 0 {
			if metaSame && imagesUnchanged && s.revision == state.paneRevision {
				return &protocol.TerminalFrame{
					Delta: true, Unchanged: true,
					Name: s.info.Name, Base: base, Offset: offset, History: history,
				}, s.revision
			}
		} else if state.offset == 0 {
			scrollUp := 0
			if !alt && curScroll > state.scrollCount {
				if d := curScroll - state.scrollCount; d < uint64(h) {
					scrollUp = int(d)
				} else {
					scrollUp = h
				}
			}
			if scrollUp < h {
				var dirtyBuf [64]int
				dirty := dirtyBuf[:0]
				for y := range h {
					var expected uint64
					if y+scrollUp < h {
						expected = state.rowRevs[y+scrollUp]
					}
					if s.term.RowRev(y) != expected {
						dirty = append(dirty, y)
					}
				}
				if scrollUp == 0 && len(dirty) == 0 && metaSame && imagesUnchanged {
					state.paneRevision = s.revision
					state.scrollCount = curScroll
					return &protocol.TerminalFrame{
						Delta: true, Unchanged: true,
						Name: s.info.Name, Base: base, Offset: offset, History: history,
					}, s.revision
				}
				if placed == nil && !state.hadImages && (scrollUp > 0 || len(dirty) < h) {
					var links []string
					if len(state.links) > 0 {
						links = append(make([]string, 0, len(state.links)+4), state.links...)
					}
					var deltas []protocol.RowDelta
					if len(dirty) > 0 {
						deltas = make([]protocol.RowDelta, len(dirty))
						for i, y := range dirty {
							row := make([]protocol.Cell, w)
							s.convertRow(row, s.term.RowCells(y), 0, y, false, bg, &links, nil)
							deltas[i] = protocol.RowDelta{
								Y:       y,
								Cells:   row,
								Wrapped: s.term.LineWrapped(y),
							}
						}
					}
					if len(links) <= 64 {
						f := &protocol.TerminalFrame{
							Delta:         true,
							ScrollUp:      scrollUp,
							RowDeltas:     deltas,
							Name:          s.info.Name,
							Title:         s.title,
							Base:          base,
							Epoch:         s.epoch,
							HistoryStart:  historyStart,
							Offset:        offset,
							History:       history,
							CursorX:       p.X,
							CursorY:       p.Y,
							CursorVisible: cursorVisible,
							CursorStyle:   cursorStyle,
							CursorBlink:   cursorBlink,
							Alt:           alt,
							Exited:        s.info.Exited,
							Mouse:         mouse,
							Links:         links,
						}
						state.record(w, h, s.epoch, alt, offset, history, base, historyStart,
							curScroll, s.revision, bg, s.paletteSeq, s.info.Name, s.title,
							p.X, p.Y, cursorVisible, cursorStyle, cursorBlink, mouse, s.info.Exited,
							s.term, links, nil)
						return f, s.revision
					}
				}
			}
		}
	}

	f := &protocol.TerminalFrame{
		Name: s.info.Name, Title: s.title, Base: base, Epoch: s.epoch, HistoryStart: historyStart,
		Offset: offset, History: history, CursorX: p.X, CursorY: p.Y,
		CursorVisible: cursorVisible, CursorStyle: cursorStyle, CursorBlink: cursorBlink,
		Alt: alt, Exited: s.info.Exited, Mouse: mouse,
	}
	defer placed.finish(f)
	f.Rows = make([][]protocol.Cell, h)
	for y := range f.Rows {
		f.Rows[y] = make([]protocol.Cell, w)
		row := history - offset + y
		wrapped := false
		if row < history {
			wrapped = s.term.ScrollbackWrapped(row)
			s.convertRow(f.Rows[y], s.term.ScrollbackRowCells(row), 0, row, true, bg, &f.Links, placed)
		} else {
			screenY := row - history
			wrapped = s.term.LineWrapped(screenY)
			s.convertRow(f.Rows[y], s.term.RowCells(screenY), 0, screenY, false, bg, &f.Links, placed)
		}
		if wrapped {
			if f.Wrapped == nil {
				f.Wrapped = make([]bool, len(f.Rows))
			}
			f.Wrapped[y] = true
		}
	}
	if state != nil {
		state.record(w, h, s.epoch, alt, offset, history, base, historyStart,
			curScroll, s.revision, bg, s.paletteSeq, s.info.Name, s.title,
			p.X, p.Y, cursorVisible, cursorStyle, cursorBlink, mouse, s.info.Exited,
			s.term, f.Links, placed)
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
	bg := s.defaultBG()
	placed := s.frameImages()
	defer placed.finish(f)
	for y := 0; y < height; y++ {
		row := make([]protocol.Cell, width)
		s.convertRow(row, s.term.RowCells(startY+y), startX, startY+y, false, bg, nil, placed)
		f.Rows = append(f.Rows, row)
	}
	return f
}
