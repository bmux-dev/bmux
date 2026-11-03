package vt

import (
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/ordered"
)

// Screen represents a virtual terminal screen.
type Screen struct {
	// cb is the callbacks struct to use.
	cb *Callbacks
	// The buffer of the screen.
	buf *uv.RenderBuffer
	// The cur of the screen.
	cur, saved Cursor
	// scroll is the scroll region.
	scroll uv.Rectangle
	// scrollback is the scrollback buffer for lines scrolled off the top.
	scrollback *Scrollback
	// bmux: flags[y] is what's known about row y besides its cells: whether
	// the terminal wrapped it onto row y+1 (reflow and copies join such
	// rows), and whether a shell prompt starts on it.
	flags []LineFlags
}

// LineFlags record what bmux knows about a line besides its cells.
// bmux: added for reflow and prompt jumps.
type LineFlags uint8

const (
	// FlagWrapped marks a line the terminal wrapped onto the next one.
	FlagWrapped LineFlags = 1 << iota
	// FlagPrompt marks a line where a shell prompt starts (OSC 133;A).
	FlagPrompt
)

// NewScreen creates a new screen.
func NewScreen(w, h int) *Screen {
	s := Screen{
		buf:        uv.NewRenderBuffer(w, h),
		scrollback: NewScrollback(DefaultScrollbackSize),
		flags:      make([]LineFlags, h),
	}
	s.scroll = s.buf.Bounds()
	return &s
}

// Reset resets the screen.
// It clears the screen, sets the cursor to the top left corner, reset the
// cursor styles, and resets the scroll region.
func (s *Screen) Reset() {
	s.buf.Clear()
	s.cur = Cursor{}
	s.saved = Cursor{}
	s.scroll = s.buf.Bounds()
	s.buf.Touched = nil
	clear(s.flags)
}

// Bounds returns the bounds of the screen.
func (s *Screen) Bounds() uv.Rectangle {
	return s.buf.Bounds()
}

// Touched returns touched lines in the screen buffer.
func (s *Screen) Touched() []*uv.LineData {
	return s.buf.Touched
}

// ClearTouched clears the touched state.
func (s *Screen) ClearTouched() {
	s.buf.Touched = nil
}

// CellAt returns the cell at the given x, y position.
func (s *Screen) CellAt(x int, y int) *uv.Cell {
	return s.buf.CellAt(x, y)
}

// SetCell sets the cell at the given x, y position.
func (s *Screen) SetCell(x, y int, c *uv.Cell) {
	s.buf.SetCell(x, y, c)
}

// Height returns the height of the screen.
func (s *Screen) Height() int {
	return s.buf.Height()
}

// Resize resizes the screen, cropping or extending rows without rewrapping.
func (s *Screen) Resize(width int, height int) {
	oldWidth := 0
	if s.buf == nil {
		s.buf = uv.NewRenderBuffer(width, height)
	} else {
		oldWidth = s.buf.Width()
		s.buf.Resize(width, height)
		s.buf.Touched = nil
	}
	s.flags = resizeFlags(s.flags, height)
	if width != oldWidth {
		// Cropped or padded rows no longer end where they wrapped.
		for y := range s.flags {
			s.flags[y] &^= FlagWrapped
		}
	}
	s.scroll = s.buf.Bounds()
}

// resizeFlags keeps the first height rows' flags, adding rows without any.
func resizeFlags(flags []LineFlags, height int) []LineFlags {
	if height <= len(flags) {
		return flags[:height]
	}
	return append(flags, make([]LineFlags, height-len(flags))...)
}

// LineFlags returns row y's flags. bmux: added for reflow and prompt jumps.
func (s *Screen) LineFlags(y int) LineFlags {
	if y >= 0 && y < len(s.flags) {
		return s.flags[y]
	}
	return 0
}

// LineWrapped reports whether row y continues onto the next row.
// bmux: added for reflow.
func (s *Screen) LineWrapped(y int) bool {
	return s.LineFlags(y)&FlagWrapped != 0
}

// setFlag turns flag on or off for row y.
func (s *Screen) setFlag(y int, flag LineFlags, on bool) {
	if y < 0 || y >= len(s.flags) {
		return
	}
	if on {
		s.flags[y] |= flag
	} else {
		s.flags[y] &^= flag
	}
}

// clearFlags forgets the wraps of rows erased through their last column,
// which end where they end now, and the prompts of rows erased whole.
func (s *Screen) clearFlags(area uv.Rectangle) {
	if area.Max.X < s.buf.Width() {
		return
	}
	drop := FlagWrapped
	if area.Min.X <= 0 {
		drop |= FlagPrompt
	}
	for y := max(0, area.Min.Y); y < min(area.Max.Y, len(s.flags)); y++ {
		s.flags[y] &^= drop
	}
}

// Width returns the width of the screen.
func (s *Screen) Width() int {
	return s.buf.Width()
}

// Clear clears the screen with blank cells.
func (s *Screen) Clear() {
	s.ClearArea(s.Bounds())
}

// ClearWithScrollback saves all non-empty lines to scrollback before clearing.
// This is used for operations like ED 2 (erase screen) where content should
// be preserved in history.
func (s *Screen) ClearWithScrollback() {
	if s.scrollback != nil {
		// Save all lines that have content before clearing
		for y := range s.buf.Height() {
			line := s.buf.Line(y)
			if line != nil && !s.isLineEmpty(line) {
				s.scrollback.PushLine(line, s.LineFlags(y))
			}
		}
	}
	s.Clear()
}

// isLineEmpty returns true if the line contains only empty/space cells.
func (s *Screen) isLineEmpty(line uv.Line) bool {
	for _, cell := range line {
		if cell.Content != "" && cell.Content != " " {
			return false
		}
	}
	return true
}

// ClearArea clears the given area.
func (s *Screen) ClearArea(area uv.Rectangle) {
	s.buf.ClearArea(area)
	s.touchArea(area)
	s.clearFlags(area)
}

// Fill fills the screen or part of it.
func (s *Screen) Fill(c *uv.Cell) {
	s.FillArea(c, s.Bounds())
}

// FillArea fills the given area with the given cell.
func (s *Screen) FillArea(c *uv.Cell, area uv.Rectangle) {
	s.buf.FillArea(c, area)
	s.touchArea(area)
	s.clearFlags(area)
}

// setHorizontalMargins sets the horizontal margins.
func (s *Screen) setHorizontalMargins(left, right int) {
	s.scroll.Min.X = left
	s.scroll.Max.X = right
}

// setVerticalMargins sets the vertical margins.
func (s *Screen) setVerticalMargins(top, bottom int) {
	s.scroll.Min.Y = top
	s.scroll.Max.Y = bottom
}

// setCursorX sets the cursor X position. If margins is true, the cursor is
// only set if it is within the scroll margins.
func (s *Screen) setCursorX(x int, margins bool) {
	s.setCursor(x, s.cur.Y, margins)
}

// setCursor sets the cursor position. If margins is true, the cursor is only
// set if it is within the scroll margins. This follows how [ansi.CUP] works.
func (s *Screen) setCursor(x, y int, margins bool) {
	old := s.cur.Position
	if !margins {
		y = ordered.Clamp(y, 0, s.buf.Height()-1)
		x = ordered.Clamp(x, 0, s.buf.Width()-1)
	} else {
		y = ordered.Clamp(s.scroll.Min.Y+y, s.scroll.Min.Y, s.scroll.Max.Y-1)
		x = ordered.Clamp(s.scroll.Min.X+x, s.scroll.Min.X, s.scroll.Max.X-1)
	}
	s.cur.X, s.cur.Y = x, y

	if s.cb.CursorPosition != nil && (old.X != x || old.Y != y) {
		s.cb.CursorPosition(old, uv.Pos(x, y))
	}
}

// moveCursor moves the cursor by the given x and y deltas. If the cursor
// position is inside the scroll region, it is bounded by the scroll region.
// Otherwise, it is bounded by the screen bounds.
// This follows how [ansi.CUU], [ansi.CUD], [ansi.CUF], [ansi.CUB], [ansi.CNL],
// [ansi.CPL].
func (s *Screen) moveCursor(dx, dy int) {
	scroll := s.scroll
	old := s.cur.Position
	if old.X < scroll.Min.X {
		scroll.Min.X = 0
	}
	if old.X >= scroll.Max.X {
		scroll.Max.X = s.buf.Width()
	}

	pt := uv.Pos(s.cur.X+dx, s.cur.Y+dy)

	var x, y int
	if old.In(scroll) {
		y = ordered.Clamp(pt.Y, scroll.Min.Y, scroll.Max.Y-1)
		x = ordered.Clamp(pt.X, scroll.Min.X, scroll.Max.X-1)
	} else {
		y = ordered.Clamp(pt.Y, 0, s.buf.Height()-1)
		x = ordered.Clamp(pt.X, 0, s.buf.Width()-1)
	}

	s.cur.X, s.cur.Y = x, y

	if s.cb.CursorPosition != nil && (old.X != x || old.Y != y) {
		s.cb.CursorPosition(old, uv.Pos(x, y))
	}
}

// Cursor returns the cursor.
func (s *Screen) Cursor() Cursor {
	return s.cur
}

// CursorPosition returns the cursor position.
func (s *Screen) CursorPosition() (x, y int) {
	return s.cur.X, s.cur.Y
}

// ScrollRegion returns the scroll region.
func (s *Screen) ScrollRegion() uv.Rectangle {
	return s.scroll
}

// SaveCursor saves the cursor.
func (s *Screen) SaveCursor() {
	s.saved = s.cur
}

// RestoreCursor restores the cursor.
func (s *Screen) RestoreCursor() {
	old := s.cur.Position
	s.cur = s.saved

	if s.cb.CursorPosition != nil && (old.X != s.cur.X || old.Y != s.cur.Y) {
		s.cb.CursorPosition(old, s.cur.Position)
	}
}

// setCursorHidden sets the cursor hidden.
func (s *Screen) setCursorHidden(hidden bool) {
	changed := s.cur.Hidden != hidden
	s.cur.Hidden = hidden
	if changed && s.cb.CursorVisibility != nil {
		s.cb.CursorVisibility(!hidden)
	}
}

// setCursorStyle sets the cursor style.
func (s *Screen) setCursorStyle(style CursorStyle, blink bool) {
	changed := s.cur.Style != style || s.cur.Steady != !blink
	s.cur.Style = style
	s.cur.Steady = !blink
	if changed && s.cb.CursorStyle != nil {
		s.cb.CursorStyle(style, !blink)
	}
}

// cursorPen returns the cursor pen.
func (s *Screen) cursorPen() uv.Style {
	return s.cur.Pen
}

// cursorLink returns the cursor link.
func (s *Screen) cursorLink() uv.Link {
	return s.cur.Link
}

// ShowCursor shows the cursor.
func (s *Screen) ShowCursor() {
	s.setCursorHidden(false)
}

// HideCursor hides the cursor.
func (s *Screen) HideCursor() {
	s.setCursorHidden(true)
}

// InsertCell inserts n blank characters at the cursor position pushing out
// cells to the right and out of the screen.
func (s *Screen) InsertCell(n int) {
	if n <= 0 {
		return
	}

	x, y := s.cur.X, s.cur.Y
	s.buf.InsertCellArea(x, y, n, s.blankCell(), s.scroll)
}

// DeleteCell deletes n cells at the cursor position moving cells to the left.
// This has no effect if the cursor is outside the scroll region.
func (s *Screen) DeleteCell(n int) {
	if n <= 0 {
		return
	}

	x, y := s.cur.X, s.cur.Y
	s.buf.DeleteCellArea(x, y, n, s.blankCell(), s.scroll)
}

// ScrollUp scrolls the content up n lines within the given region. Lines
// scrolled past the top margin are lost. This is equivalent to [ansi.SU] which
// moves the cursor to the top margin and performs a [ansi.DL] operation.
func (s *Screen) ScrollUp(n int) {
	x, y := s.CursorPosition()
	s.setCursor(s.cur.X, 0, true)
	s.DeleteLine(n)
	s.setCursor(x, y, false)
}

// ScrollDown scrolls the content down n lines within the given region. Lines
// scrolled past the bottom margin are lost. This is equivalent to [ansi.SD]
// which moves the cursor to top margin and performs a [ansi.IL] operation.
func (s *Screen) ScrollDown(n int) {
	x, y := s.CursorPosition()
	s.setCursor(s.cur.X, 0, true)
	s.InsertLine(n)
	s.setCursor(x, y, false)
}

// InsertLine inserts n blank lines at the cursor position Y coordinate.
// Only operates if cursor is within scroll region. Lines below cursor Y
// are moved down, with those past bottom margin being discarded.
// It returns true if the operation was successful.
func (s *Screen) InsertLine(n int) bool {
	if n <= 0 {
		return false
	}

	x, y := s.cur.X, s.cur.Y

	// Only operate if cursor Y is within scroll region
	if y < s.scroll.Min.Y || y >= s.scroll.Max.Y ||
		x < s.scroll.Min.X || x >= s.scroll.Max.X {
		return false
	}

	s.moveLines(y, n, s.scroll)
	s.shiftFlags(y, n)

	return true
}

// DeleteLine deletes n lines at the cursor position Y coordinate.
// Only operates if cursor is within scroll region. Lines below cursor Y
// are moved up, with blank lines inserted at the bottom of scroll region.
// If scrollback is enabled and cursor is at top of scroll region, lines
// are saved to the scrollback buffer before deletion.
// It returns true if the operation was successful.
func (s *Screen) DeleteLine(n int) bool {
	if n <= 0 {
		return false
	}

	scroll := s.scroll
	x, y := s.cur.X, s.cur.Y

	// Only operate if cursor Y is within scroll region
	if y < scroll.Min.Y || y >= scroll.Max.Y ||
		x < scroll.Min.X || x >= scroll.Max.X {
		return false
	}

	// Save lines to scrollback if we're at the top of the scroll region
	// and the scroll region uses the full width (typical terminal scroll).
	// This captures lines that would be lost during scroll up operations.
	if s.scrollback != nil && y == scroll.Min.Y &&
		scroll.Min.X == 0 && scroll.Max.X == s.buf.Width() {
		// Save lines that will be deleted
		linesToSave := min(n, scroll.Max.Y-y)
		s.scrollback.PushN(s.buf, s.flags, y, linesToSave)
	}

	s.moveLines(y, -n, s.scroll)
	s.shiftFlags(y, -n)

	return true
}

// blankCell returns the cursor blank cell with the background color set to the
// current pen background color. If the pen background color is nil, the return
// value is nil.
func (s *Screen) blankCell() *uv.Cell {
	if s.cur.Pen.Bg == nil {
		return nil
	}

	c := uv.EmptyCell
	c.Style.Bg = s.cur.Pen.Bg
	return &c
}

// moveLines inserts (n > 0) or deletes (n < 0) |n| blank lines at row y of
// area, like uv's InsertLineArea and DeleteLineArea. When the area spans the
// full width, which every ordinary scroll does, it moves whole rows by
// rotating their slices instead of copying every cell: printing a line at the
// bottom of the screen then costs one row of cells, not the whole screen.
// bmux: added for output throughput.
func (s *Screen) moveLines(y, n int, area uv.Rectangle) {
	if area.Min.X != 0 || area.Max.X != s.buf.Width() {
		if n > 0 {
			s.buf.InsertLineArea(y, n, s.blankCell(), area)
		} else {
			s.buf.DeleteLineArea(y, -n, s.blankCell(), area)
		}
		return
	}
	if n == 0 || y < area.Min.Y || y >= area.Max.Y || y >= s.buf.Height() {
		return
	}
	rows := s.buf.Lines[y:area.Max.Y]
	k := min(max(n, -n), len(rows))
	// Rotate right by k to insert, left by k to delete, without allocating.
	split := len(rows) - k
	if n < 0 {
		split = k
	}
	slices.Reverse(rows[:split])
	slices.Reverse(rows[split:])
	slices.Reverse(rows)
	fresh := rows[:k]
	if n < 0 {
		fresh = rows[len(rows)-k:]
	}
	blank := uv.EmptyCell
	if c := s.blankCell(); c != nil {
		blank = *c
	}
	for _, row := range fresh {
		for x := range row {
			row[x] = blank
		}
	}
	s.touchArea(uv.Rect(0, y, s.buf.Width(), len(rows)))
}

// touchArea marks all lines in the given area as touched.
func (s *Screen) touchArea(area uv.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		s.buf.TouchLine(area.Min.X, y, area.Max.X-area.Min.X)
	}
}

// shiftFlags moves line flags along with rows inserted (n > 0) or deleted
// (n < 0) at row y inside the scroll region. A region narrower than the
// screen moves only part of each row, so those rows' flags are forgotten.
// bmux: added for reflow.
func (s *Screen) shiftFlags(y, n int) {
	top, bottom := y, min(s.scroll.Max.Y, len(s.flags))
	if top >= bottom {
		return
	}
	region := s.flags[top:bottom]
	if s.scroll.Min.X != 0 || s.scroll.Max.X != s.buf.Width() {
		clear(region)
		return
	}
	if n > 0 {
		n = min(n, len(region))
		copy(region[n:], region[:len(region)-n])
		clear(region[:n])
	} else {
		n = min(-n, len(region))
		copy(region, region[n:])
		clear(region[len(region)-n:])
	}
}

// Scrollback returns the screen's scrollback buffer.
func (s *Screen) Scrollback() *Scrollback {
	return s.scrollback
}

// SetScrollback sets the screen's scrollback buffer.
// Pass nil to disable scrollback.
func (s *Screen) SetScrollback(sb *Scrollback) {
	s.scrollback = sb
}

// SetScrollbackSize sets the maximum number of lines in the scrollback buffer.
func (s *Screen) SetScrollbackSize(maxLines int) {
	if s.scrollback == nil {
		s.scrollback = NewScrollback(maxLines)
	} else {
		s.scrollback.SetMaxLines(maxLines)
	}
}
