package vt

import (
	"image/color"
	"io"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/screen"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

const (
	// maxParserDataSize caps OSC/DCS/APC/PM/SOS payloads to 4 MiB, matching the
	// previous fixed limit without eagerly allocating 4 MiB per pane.
	maxParserDataSize = 4 << 20
	// parserDataShrinkThreshold releases large one-off payload buffers (> 4 KiB)
	// back to the GC after dispatch so panes stay lightweight.
	parserDataShrinkThreshold = 4 << 10
	// styleCompactHighWater triggers live style compaction only under extreme
	// unique-style churn (e.g. adversarial 24-bit RGB streams).
	styleCompactHighWater = 65536
)

// Logger represents a logger interface.
type Logger interface {
	Printf(format string, v ...any)
}

// Emulator represents a virtual terminal emulator.
type Emulator struct {
	handlers

	// The terminal's indexed 256 colors.
	colors [256]color.Color

	// Shared interned style table for main screen, alt screen, and scrollback.
	styles StyleTable

	// Both main and alt screens and a pointer to the currently active screen.
	scrs [2]Screen
	scr  *Screen

	// Character sets
	charsets [4]CharSet

	// log is the logger to use.
	logger Logger

	// terminal default colors.
	defaultFg, defaultBg, defaultCur color.Color
	fgColor, bgColor, curColor       color.Color

	// Terminal modes.
	modes ansi.Modes

	// Keyboard protocols the program asked for: a kitty flag stack per
	// screen, and xterm's modifyOtherKeys level.
	kitty           [2]kittyKeyboard
	modifyOtherKeys int

	// The last written character.
	lastChar rune // either ansi.Rune or ansi.Grapheme
	// The bytes of a grapheme cluster still being composed. Bytes rather than
	// runes, so the segmenter can read the buffer without a conversion on
	// every character.
	grapheme []byte

	// The ANSI parser to use, with lazy-growing string data buffer.
	parser        *ansi.Parser
	parserDataLen int
	cb            Callbacks

	// The terminal's icon name and title.
	iconName, title string
	// The current reported working directory. This is not validated.
	cwd string

	// tabstop is the list of tab stops.
	tabstops *uv.TabStops

	// I/O pipes.
	pr *io.PipeReader
	pw *io.PipeWriter

	// The GL and GR character set identifiers.
	gl, gr  int
	gsingle int // temporarily select GL or GR

	// Indicates if the terminal is closed.
	closed bool

	// graphics handles Kitty graphics commands; nil drops them.
	graphics Graphics
	// The attached terminal's cell size in pixels; 0 when unknown.
	cellWidth, cellHeight int

	// atPhantom indicates if the cursor is out of bounds.
	// When true, and a character is written, the cursor is moved to the next line.
	atPhantom bool

	// Cached hot mode booleans updated by setMode to avoid interface-keyed
	// map lookups on every printed character or cursor/newline operation.
	insert           bool // ansi.ModeInsertReplace (IRM)
	autoWrap         bool // ansi.ModeAutoWrap (DECAWM)
	origin           bool // ansi.ModeOrigin (DECOM)
	lineFeedNewLine  bool // ansi.ModeLineFeedNewLine (LNM)
	leftRightMargin  bool // ansi.ModeLeftRightMargin (DECLRMM)
	nextStyleCompact int  // dynamic high-water mark for style table GC
}

var _ Terminal = (*Emulator)(nil)

// NewEmulator creates a new virtual terminal emulator.
func NewEmulator(w, h int) *Emulator {
	t := new(Emulator)
	t.scrs[0] = *newScreen(w, h, &t.styles, true)
	// Alternate screen never retains a scrollback buffer.
	t.scrs[1] = *newScreen(w, h, &t.styles, false)
	t.scr = &t.scrs[0]
	t.scrs[0].cb = &t.cb
	t.scrs[1].cb = &t.cb

	// Initialize parser with a lazy-growing data buffer (0 bytes initially,
	// growing on demand up to maxParserDataSize = 4 MiB, shrunk after >4 KiB).
	t.parser = new(ansi.Parser)
	t.parser.SetParamsSize(parser.MaxParamsSize)
	t.parser.SetDataSize(0)
	t.parser.Reset()
	t.parser.SetHandler(ansi.Handler{
		Print:     t.handlePrint,
		Execute:   t.handleControl,
		HandleCsi: t.handleCsi,
		HandleEsc: t.handleEsc,
		HandleDcs: t.handleDcs,
		HandleOsc: t.handleOsc,
		HandleApc: t.handleApc,
		HandlePm:  t.handlePm,
		HandleSos: t.handleSos,
	})
	t.pr, t.pw = io.Pipe()
	t.resetModes()
	t.tabstops = uv.DefaultTabStops(w)
	t.registerDefaultHandlers()

	// Default colors
	t.defaultFg = color.White
	t.defaultBg = color.Black
	t.defaultCur = color.White

	return t
}

// SetLogger sets the terminal's logger.
func (e *Emulator) SetLogger(l Logger) {
	e.logger = l
}

// SetCallbacks sets the terminal's callbacks.
func (e *Emulator) SetCallbacks(cb Callbacks) {
	e.cb = cb
	e.scrs[0].cb = &e.cb
	e.scrs[1].cb = &e.cb
}

// String returns a string representation of the underlying screen buffer.
func (e *Emulator) String() string {
	return uv.TrimSpace(e.scr.String())
}

// Render renders a snapshot of the terminal screen as a string with styles and
// links encoded as ANSI escape codes.
func (e *Emulator) Render() string {
	return e.scr.Render()
}

var _ uv.Screen = (*Emulator)(nil)

// Bounds returns the bounds of the terminal.
func (e *Emulator) Bounds() uv.Rectangle {
	return e.scr.Bounds()
}

// CellAt returns the current focused screen cell at the given x, y position.
// It returns nil if the cell is out of bounds.
func (e *Emulator) CellAt(x, y int) *uv.Cell {
	return e.scr.CellAt(x, y)
}

// SetCell sets the current focused screen cell at the given x, y position.
func (e *Emulator) SetCell(x, y int, c *uv.Cell) {
	e.scr.SetCell(x, y, c)
}

// Style returns the interned Style for id (0 is the default plain Style).
func (e *Emulator) Style(id uint32) Style {
	return e.styles.Get(id)
}

// StyleTableVersion returns a monotonic counter incremented only if internal
// style IDs are compacted/renumbered.
func (e *Emulator) StyleTableVersion() uint64 {
	return e.styles.Version()
}

// RowCells returns a zero-alloc read-only slice of compact Cells for row y of
// the active screen, or nil if out of bounds.
func (e *Emulator) RowCells(y int) []Cell {
	return e.scr.RowCells(y)
}

// CellGrapheme returns the multi-rune grapheme cluster string at (x, y) on the
// active screen, or "" if the cell is single-rune or out of bounds.
func (e *Emulator) CellGrapheme(x, y int) string {
	return e.scr.CellGrapheme(x, y)
}

// CellLink returns the OSC 8 hyperlink at (x, y) on the active screen, or the
// zero uv.Link if none.
func (e *Emulator) CellLink(x, y int) uv.Link {
	return e.scr.CellLink(x, y)
}

// ScrollbackRowCells returns a zero-alloc read-only slice of compact Cells for
// scrollback line y (0 = oldest), or nil if out of bounds or empty.
func (e *Emulator) ScrollbackRowCells(y int) []Cell {
	return e.Scrollback().RowCells(y)
}

// ScrollbackCellGrapheme returns the multi-rune grapheme cluster string at
// (x, y) in scrollback (y = 0 is oldest), or "".
func (e *Emulator) ScrollbackCellGrapheme(x, y int) string {
	return e.Scrollback().CellGrapheme(x, y)
}

// ScrollbackCellLink returns the OSC 8 hyperlink at (x, y) in scrollback
// (y = 0 is oldest), or the zero uv.Link.
func (e *Emulator) ScrollbackCellLink(x, y int) uv.Link {
	return e.Scrollback().CellLink(x, y)
}

// RowRev returns the per-row monotonic revision of row y on the active screen.
func (e *Emulator) RowRev(y int) uint64 {
	return e.scr.RowRev(y)
}

// ScrollCount returns the monotonic full-screen scroll-up line counter for the
// main screen.
func (e *Emulator) ScrollCount() uint64 {
	return e.scrs[0].ScrollCount()
}

// ScrollbackTotal returns the monotonic total number of lines ever pushed into
// the main screen's scrollback buffer.
func (e *Emulator) ScrollbackTotal() uint64 {
	return e.Scrollback().TotalPushed()
}

// ScrollbackEpoch returns a monotonic counter incremented whenever the main
// screen's scrollback is cleared or rewritten by reflow.
func (e *Emulator) ScrollbackEpoch() uint64 {
	return e.Scrollback().Epoch()
}

// HasScrollback reports whether the main screen has a scrollback buffer.
func (e *Emulator) HasScrollback() bool {
	return e.scrs[0].HasScrollback()
}

// compactStylesIfNeeded garbage-collects unreferenced StyleIDs if the interned
// style table grows beyond the dynamic high-water threshold.
func (e *Emulator) compactStylesIfNeeded() {
	n := len(e.styles.styles)
	threshold := e.nextStyleCompact
	if threshold < styleCompactHighWater {
		threshold = styleCompactHighWater
	}
	if n <= threshold {
		return
	}
	used := make([]bool, n)
	used[0] = true
	e.scrs[0].markUsedStyles(used)
	e.scrs[1].markUsedStyles(used)

	remap := make([]uint32, n)
	newStyles := make([]Style, 1, 64)
	newIndex := make(map[Style]uint32, 64)
	for id := 1; id < n; id++ {
		if !used[id] {
			continue
		}
		st := e.styles.styles[id]
		newID := uint32(len(newStyles))
		newStyles = append(newStyles, st)
		newIndex[st] = newID
		remap[id] = newID
	}
	e.scrs[0].remapStyles(remap)
	e.scrs[1].remapStyles(remap)
	e.styles.styles = newStyles
	e.styles.index = newIndex
	e.styles.version++
	e.nextStyleCompact = max(styleCompactHighWater, len(newStyles)*2)
}

// WidthMethod returns the width method used by the terminal.
func (e *Emulator) WidthMethod() uv.WidthMethod {
	if e.isModeSet(ansi.ModeUnicodeCore) {
		return ansi.GraphemeWidth
	}
	return ansi.WcWidth
}

// Draw implements the [uv.Drawable] interface.
func (e *Emulator) Draw(scr uv.Screen, area uv.Rectangle) {
	bg := uv.EmptyCell
	bg.Style.Bg = e.BackgroundColor()
	screen.FillArea(scr, &bg, area)
	for y := range e.Height() {
		for x := 0; x < e.Width(); {
			w := 1
			cell := e.CellAt(x, y)
			if cell != nil {
				if cell.Width > 1 {
					w = cell.Width
				}
				if cell.Style.Bg == nil && e.bgColor != nil {
					cell.Style.Bg = e.bgColor
				}
				if cell.Style.Fg == nil && e.fgColor != nil {
					cell.Style.Fg = e.fgColor
				}
				scr.SetCell(x+area.Min.X, y+area.Min.Y, cell)
			}
			x += w
		}
	}
}

// Height returns the height of the terminal.
func (e *Emulator) Height() int {
	return e.scr.Height()
}

// Width returns the width of the terminal.
func (e *Emulator) Width() int {
	return e.scr.Width()
}

// CursorPosition returns the terminal's cursor position.
func (e *Emulator) CursorPosition() uv.Position {
	x, y := e.scr.CursorPosition()
	return uv.Pos(x, y)
}

// Resize resizes the terminal. bmux: the main screen reflows, whether or not
// it is showing; the alternate screen is cropped, since full-screen programs
// redraw at the new size.
func (e *Emulator) Resize(width int, height int) {
	main := &e.scrs[0]
	phantom := main.reflow(width, height, e.atPhantom && e.scr == main)
	if e.scr == main {
		e.atPhantom = phantom
	} else {
		x, y := e.scr.CursorPosition()
		if e.atPhantom {
			if x < width-1 {
				e.atPhantom = false
				x++
			}
		}
		y = max(0, min(y, height-1))
		x = max(0, min(x, width-1))
		e.scrs[1].Resize(width, height)
		e.setCursor(x, y)
	}
	if e.scr == main {
		e.scrs[1].Resize(width, height)
	}
	e.tabstops = uv.DefaultTabStops(width)

	if e.isModeSet(ansi.ModeInBandResize) {
		pw, ph := e.pixelSize()
		_, _ = io.WriteString(e.pw, ansi.InBandResize(e.Height(), e.Width(), ph, pw))
	}
}

// LineWrapped reports whether row y of the showing screen continues onto the
// next row because the terminal wrapped it. bmux: added for reflow.
func (e *Emulator) LineWrapped(y int) bool {
	return e.scr.LineWrapped(y)
}

// ScrollbackWrapped reports whether scrollback line y (0 = oldest) continues
// onto the next one. bmux: added for reflow.
func (e *Emulator) ScrollbackWrapped(y int) bool {
	return e.Scrollback().Wrapped(y)
}

// LinePrompt reports whether a shell prompt starts on row y of the showing
// screen. bmux: added for prompt jumps.
func (e *Emulator) LinePrompt(y int) bool {
	return e.scr.LineFlags(y)&FlagPrompt != 0
}

// ScrollbackPrompt reports whether a shell prompt starts on scrollback line
// y (0 = oldest). bmux: added for prompt jumps.
func (e *Emulator) ScrollbackPrompt(y int) bool {
	return e.Scrollback().LineFlags(y)&FlagPrompt != 0
}

// MarkPrompt records that a shell prompt starts on the cursor's row of the
// main screen, as shells report with OSC 133;A. The alternate screen has no
// history, so prompts there aren't kept. bmux: added for prompt jumps.
func (e *Emulator) MarkPrompt() {
	if e.scr != &e.scrs[0] {
		return
	}
	_, y := e.scr.CursorPosition()
	e.scr.setFlag(y, FlagPrompt, true)
}

// Read reads data from the terminal input buffer.
func (e *Emulator) Read(p []byte) (n int, err error) {
	if e.closed {
		return 0, io.EOF
	}

	return e.pr.Read(p) //nolint:wrapcheck
}

// Close closes the terminal.
func (e *Emulator) Close() error {
	if e.closed {
		return nil
	}

	e.closed = true
	return e.pw.CloseWithError(io.EOF) //nolint:wrapcheck
}

func isParserStringState(st parser.State) bool {
	switch st {
	case parser.OscStringState, parser.DcsStringState, parser.SosStringState, parser.PmStringState, parser.ApcStringState:
		return true
	default:
		return false
	}
}

// Write writes data to the terminal output buffer.
func (e *Emulator) Write(p []byte) (n int, err error) {
	if e.closed {
		return 0, io.ErrClosedPipe
	}

	for i := 0; i < len(p); {
		st := e.parser.State()
		if st == parser.GroundState {
			b := p[i]
			if b >= ansi.SP && b < ansi.DEL {
				if e.gsingle == 0 && e.charsets[e.gl] == nil && !e.insert && e.scr.cur.Link.IsZero() {
					end := i + 1
					for end < len(p) && p[end] >= ansi.SP && p[end] < ansi.DEL {
						end++
					}
					runEnd := end
					// Hold back the last ASCII byte if a non-ASCII byte follows
					// so a combining mark can still join it in e.grapheme.
					if end < len(p) && p[end] >= ansi.DEL {
						runEnd = end - 1
					}
					if runEnd > i {
						if len(e.grapheme) > 0 {
							e.flushGrapheme()
						}
						e.writeASCIISpan(p[i:runEnd])
						i = runEnd
						continue
					}
				}
			} else if b == ansi.CR {
				if len(e.grapheme) > 0 {
					e.flushGrapheme()
				}
				e.carriageReturn()
				i++
				continue
			} else if b == ansi.LF {
				if len(e.grapheme) > 0 {
					e.flushGrapheme()
				}
				e.linefeed()
				i++
				continue
			}
		} else if e.parserDataLen >= maxParserDataSize && isParserStringState(st) {
			// Cap string payloads at maxParserDataSize (4 MiB), matching the
			// legacy fixed-size parser buffer while letting terminators pass.
			if _, act := parser.Table.Transition(st, p[i]); act == parser.PutAction {
				i++
				continue
			}
		}

		act := e.parser.Advance(p[i])
		i++

		switch act {
		case parser.StartAction:
			e.parserDataLen = 0
		case parser.PutAction:
			e.parserDataLen++
		default:
			if e.parserDataLen > 0 && !isParserStringState(e.parser.State()) {
				if e.parserDataLen > parserDataShrinkThreshold {
					e.parser.SetDataSize(0)
				}
				e.parserDataLen = 0
			}
		}
	}

	// Flush the last cluster once the whole slice is written. Every sequence
	// handler flushes on its own way in, so a cluster only has to survive
	// until either the next printable character extends it or the write ends.
	if len(e.grapheme) > 0 {
		e.flushGrapheme()
	}
	return len(p), nil
}

// WriteString writes a string to the terminal output buffer.
func (e *Emulator) WriteString(s string) (n int, err error) {
	return e.Write([]byte(s))
}

// InputPipe returns the terminal's input pipe.
// This can be used to send input to the terminal.
func (e *Emulator) InputPipe() io.Writer {
	return e.pw
}

// Paste pastes text into the terminal.
// If bracketed paste mode is enabled, the text is bracketed with the
// appropriate escape sequences.
func (e *Emulator) Paste(text string) {
	if e.isModeSet(ansi.ModeBracketedPaste) {
		_, _ = io.WriteString(e.pw, ansi.BracketedPasteStart)
		defer io.WriteString(e.pw, ansi.BracketedPasteEnd) //nolint:errcheck
	}

	_, _ = io.WriteString(e.pw, text)
}

// SendText sends arbitrary text to the terminal.
func (e *Emulator) SendText(text string) {
	_, _ = io.WriteString(e.pw, text)
}

// SendKeys sends multiple keys to the terminal.
func (e *Emulator) SendKeys(keys ...uv.KeyEvent) {
	for _, k := range keys {
		e.SendKey(k)
	}
}

// ForegroundColor returns the terminal's foreground color. This returns nil if
// the foreground color is not set which means the outer terminal color is
// used.
func (e *Emulator) ForegroundColor() color.Color {
	if e.fgColor == nil {
		return e.defaultFg
	}
	return e.fgColor
}

// SetForegroundColor sets the terminal's foreground color.
func (e *Emulator) SetForegroundColor(c color.Color) {
	if c == nil {
		c = e.defaultFg
	}
	e.fgColor = c
	if e.cb.ForegroundColor != nil {
		e.cb.ForegroundColor(c)
	}
}

// SetDefaultForegroundColor sets the terminal's default foreground color.
func (e *Emulator) SetDefaultForegroundColor(c color.Color) {
	if c == nil {
		c = color.White
	}
	e.defaultFg = c
}

// BackgroundColor returns the terminal's background color. This returns nil if
// the background color is not set which means the outer terminal color is
// used.
func (e *Emulator) BackgroundColor() color.Color {
	if e.bgColor == nil {
		return e.defaultBg
	}
	return e.bgColor
}

// SetBackgroundColor sets the terminal's background color.
func (e *Emulator) SetBackgroundColor(c color.Color) {
	if c == nil {
		c = e.defaultBg
	}
	e.bgColor = c
	if e.cb.BackgroundColor != nil {
		e.cb.BackgroundColor(c)
	}
}

// SetDefaultBackgroundColor sets the terminal's default background color.
func (e *Emulator) SetDefaultBackgroundColor(c color.Color) {
	if c == nil {
		c = color.Black
	}
	e.defaultBg = c
}

// CursorColor returns the terminal's cursor color. This returns nil if the
// cursor color is not set which means the outer terminal color is used.
func (e *Emulator) CursorColor() color.Color {
	if e.curColor == nil {
		return e.defaultCur
	}
	return e.curColor
}

// SetCursorColor sets the terminal's cursor color.
func (e *Emulator) SetCursorColor(c color.Color) {
	if c == nil {
		c = e.defaultCur
	}
	e.curColor = c
	if e.cb.CursorColor != nil {
		e.cb.CursorColor(c)
	}
}

// SetDefaultCursorColor sets the terminal's default cursor color.
func (e *Emulator) SetDefaultCursorColor(c color.Color) {
	if c == nil {
		c = color.White
	}
	e.defaultCur = c
}

// IndexedColor returns a terminal's indexed color. An indexed color is a color
// between 0 and 255.
func (e *Emulator) IndexedColor(i int) color.Color {
	if i < 0 || i > 255 {
		return nil
	}

	c := e.colors[i]
	if c == nil {
		// Return the default color.
		return ansi.IndexedColor(i)
	}

	return c
}

// SetIndexedColor sets a terminal's indexed color.
// The index must be between 0 and 255.
func (e *Emulator) SetIndexedColor(i int, c color.Color) {
	if i < 0 || i > 255 {
		return
	}

	e.colors[i] = c
}

// resetTabStops resets the terminal tab stops to the default set.
func (e *Emulator) resetTabStops() {
	e.tabstops = uv.DefaultTabStops(e.Width())
}

func (e *Emulator) logf(format string, v ...any) {
	if e.logger != nil {
		e.logger.Printf(format, v...)
	}
}

// Scrollback returns the scrollback buffer for the main screen.
func (e *Emulator) Scrollback() *Scrollback {
	return e.scrs[0].Scrollback()
}

// ScrollbackLen returns the number of lines in the scrollback buffer.
func (e *Emulator) ScrollbackLen() int {
	sb := e.Scrollback()
	if sb == nil {
		return 0
	}
	return sb.Len()
}

// ScrollbackCellAt returns the cell at the given position in the scrollback buffer.
// x is the column, y is the line index (0 = oldest line in scrollback).
// Returns nil if position is out of bounds.
func (e *Emulator) ScrollbackCellAt(x, y int) *uv.Cell {
	sb := e.Scrollback()
	if sb == nil {
		return nil
	}
	return sb.CellAt(x, y)
}

// SetScrollbackSize sets the maximum number of lines in the scrollback buffer.
func (e *Emulator) SetScrollbackSize(maxLines int) {
	e.scrs[0].SetScrollbackSize(maxLines)
}

// ClearScrollback clears the scrollback buffer.
func (e *Emulator) ClearScrollback() {
	sb := e.Scrollback()
	if sb != nil {
		sb.Clear()
	}
}

// IsAltScreen returns whether the terminal is in alternate screen mode.
func (e *Emulator) IsAltScreen() bool {
	return e.scr == &e.scrs[1]
}
