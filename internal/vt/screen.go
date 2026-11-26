package vt

import (
	"slices"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/exp/ordered"
)

// Screen represents a virtual terminal screen backed by compact pointer-free Cells.
type Screen struct {
	// cb is the callbacks struct to use.
	cb *Callbacks

	// width and height are the screen dimensions in cells.
	width, height int
	// rows[y] holds width compact pointer-free Cell values (GC noscan).
	rows [][]Cell
	// extras holds sparse per-row maps for rare multi-rune graphemes and
	// OSC 8 hyperlinks; nil/absent for rows without any.
	extras map[int]rowExtra

	// rowRev[y] is a strictly unique monotonic revision for row y.
	// Rotating full-width rows rotates rowRev with them and assigns fresh
	// revisions to newly blanked rows, so both scroll-aware and naive row
	// diff callers detect every visible change.
	rowRev []uint64
	revSeq uint64
	// scrollCount counts full-screen scroll-up lines monotonically.
	scrollCount uint64

	// styles is the shared interned StyleTable (owned by Emulator or standalone Screen).
	styles *StyleTable

	// The cur of the screen.
	cur, saved Cursor
	// curStyleID caches styles.Intern(StyleFromUV(cur.Pen)).
	curStyleID uint32
	// curBlankStyleID caches styles.Intern(Style{Bg: ColorFromStd(cur.Pen.Bg)}).
	curBlankStyleID uint32

	// scroll is the scroll region.
	scroll uv.Rectangle
	// scrollback is the scrollback buffer for lines scrolled off the top (nil on alt screen).
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

// NewScreen creates a new screen with default scrollback enabled.
func NewScreen(w, h int) *Screen {
	return newScreen(w, h, nil, true)
}

func newScreen(w, h int, styles *StyleTable, withScrollback bool) *Screen {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	if styles == nil {
		styles = &StyleTable{}
	}
	var sb *Scrollback
	if withScrollback {
		sb = newScrollback(DefaultScrollbackSize, styles)
	}
	s := Screen{
		width:      w,
		height:     h,
		rows:       make([][]Cell, h),
		rowRev:     make([]uint64, h),
		styles:     styles,
		scrollback: sb,
		flags:      make([]LineFlags, h),
	}
	for y := range h {
		s.rows[y] = newCompactRow(w)
		s.revSeq++
		s.rowRev[y] = s.revSeq
	}
	s.scroll = s.Bounds()
	return &s
}

// bumpRow assigns a fresh unique monotonic revision to row y.
func (s *Screen) bumpRow(y int) {
	if y >= 0 && y < len(s.rowRev) {
		s.revSeq++
		s.rowRev[y] = s.revSeq
	}
}

// bumpAllRows assigns fresh unique revisions to every row.
func (s *Screen) bumpAllRows() {
	for y := range s.rowRev {
		s.revSeq++
		s.rowRev[y] = s.revSeq
	}
}

// RowRev returns the monotonic revision of row y, or 0 if out of bounds.
func (s *Screen) RowRev(y int) uint64 {
	if y < 0 || y >= len(s.rowRev) {
		return 0
	}
	return s.rowRev[y]
}

// ScrollCount returns the monotonic count of full-screen scroll-up lines.
func (s *Screen) ScrollCount() uint64 {
	return s.scrollCount
}

// Style returns the Style for interned id.
func (s *Screen) Style(id uint32) Style {
	return s.styles.Get(id)
}

// RowCells returns a zero-alloc read-only slice of compact Cells for row y,
// or nil if out of bounds.
func (s *Screen) RowCells(y int) []Cell {
	if y < 0 || y >= s.height {
		return nil
	}
	return s.rows[y]
}

// CellGrapheme returns the multi-rune grapheme cluster string at (x, y), or "".
func (s *Screen) CellGrapheme(x, y int) string {
	if len(s.extras) == 0 {
		return ""
	}
	if ex := s.extras[y]; ex != nil {
		return ex[x].grapheme
	}
	return ""
}

// CellLink returns the OSC 8 hyperlink at (x, y), or the zero uv.Link.
func (s *Screen) CellLink(x, y int) uv.Link {
	if len(s.extras) == 0 {
		return uv.Link{}
	}
	if ex := s.extras[y]; ex != nil {
		return ex[x].link
	}
	return uv.Link{}
}

// HasScrollback reports whether this screen retains scrollback history.
func (s *Screen) HasScrollback() bool {
	return s.scrollback != nil
}

// syncCursorStyle updates cached style IDs after cur.Pen changes.
func (s *Screen) syncCursorStyle() {
	if s.cur.Pen.IsZero() {
		s.curStyleID = 0
		s.curBlankStyleID = 0
		return
	}
	st := StyleFromUV(s.cur.Pen)
	s.curStyleID = s.styles.Intern(st)
	if st.Bg.IsDefault() {
		s.curBlankStyleID = 0
	} else {
		s.curBlankStyleID = s.styles.Intern(Style{Bg: st.Bg})
	}
}

func (s *Screen) setCellExtra(y, x int, ex cellExtra) {
	if s.extras == nil {
		s.extras = make(map[int]rowExtra)
	}
	re := s.extras[y]
	if re == nil {
		re = make(rowExtra)
		s.extras[y] = re
	}
	re[x] = ex
}

func (s *Screen) clearCellExtra(y, x int) {
	if len(s.extras) == 0 {
		return
	}
	re := s.extras[y]
	if re == nil {
		return
	}
	delete(re, x)
	if len(re) == 0 {
		delete(s.extras, y)
	}
}

func (s *Screen) clearRowExtrasRange(y, minX, maxX int) {
	if len(s.extras) == 0 {
		return
	}
	re := s.extras[y]
	if re == nil {
		return
	}
	if minX <= 0 && maxX >= s.width {
		delete(s.extras, y)
		return
	}
	for x := range re {
		if x >= minX && x < maxX {
			delete(re, x)
		}
	}
	if len(re) == 0 {
		delete(s.extras, y)
	}
}

// clearWideOverlap clears any wide character that occupies column x on row y,
// matching uv.Line.Set's partial-overwrite blanking behavior.
func (s *Screen) clearWideOverlap(y, x int) {
	row := s.rows[y]
	prev := row[x]
	if pw := prev.Width(); pw > 1 {
		blank := Cell{Rune: ' ', StyleID: prev.StyleID, Flags: cellWidth1}
		limit := min(x+pw, s.width)
		for j := x; j < limit; j++ {
			if row[j].HasExtra() {
				s.clearCellExtra(y, j)
			}
			row[j] = blank
		}
	} else if pw == 0 && prev.IsZero() {
		for j := 1; x-j >= 0; j++ {
			wide := row[x-j]
			if ww := wide.Width(); ww > 1 && j < ww {
				blank := Cell{Rune: ' ', StyleID: wide.StyleID, Flags: cellWidth1}
				limit := min(x-j+ww, s.width)
				for k := x - j; k < limit; k++ {
					if row[k].HasExtra() {
						s.clearCellExtra(y, k)
					}
					row[k] = blank
				}
				break
			}
		}
	}
}

// setCompactCell writes a compact cell (plus optional side-table extra) at (x, y)
// and bumps row y's revision.
func (s *Screen) setCompactCell(x, y int, c Cell, ex cellExtra, hasExtra bool) {
	if y < 0 || y >= s.height || x < 0 || x >= s.width {
		return
	}
	row := s.rows[y]
	prev := row[x]
	if prev.Width() != 1 {
		s.clearWideOverlap(y, x)
	} else if prev.HasExtra() && !hasExtra {
		s.clearCellExtra(y, x)
	}

	if c.IsZero() {
		row[x] = Cell{}
		s.bumpRow(y)
		return
	}

	cw := c.Width()
	if cw > 1 {
		if x+cw > s.width {
			blank := Cell{Rune: ' ', StyleID: c.StyleID, Flags: cellWidth1}
			for i := x; i < s.width; i++ {
				if row[i].Width() != 1 {
					s.clearWideOverlap(y, i)
				} else if row[i].HasExtra() {
					s.clearCellExtra(y, i)
				}
				row[i] = blank
			}
			s.bumpRow(y)
			return
		}
		for j := 1; j < cw; j++ {
			col := x + j
			if row[col].Width() != 1 {
				s.clearWideOverlap(y, col)
			} else if row[col].HasExtra() {
				s.clearCellExtra(y, col)
			}
			row[col] = Cell{}
		}
	}

	row[x] = c
	if hasExtra {
		s.setCellExtra(y, x, ex)
	}
	s.bumpRow(y)
}

// Reset resets the screen.
// It clears the screen, sets the cursor to the top left corner, resets the
// cursor styles, and resets the scroll region.
func (s *Screen) Reset() {
	for y := range s.height {
		row := s.rows[y]
		for x := range row {
			row[x] = EmptyCell
		}
	}
	s.extras = nil
	s.cur = Cursor{}
	s.saved = Cursor{}
	s.curStyleID = 0
	s.curBlankStyleID = 0
	s.scroll = s.Bounds()
	clear(s.flags)
	s.bumpAllRows()
}

// Bounds returns the bounds of the screen.
func (s *Screen) Bounds() uv.Rectangle {
	return uv.Rect(0, 0, s.width, s.height)
}

// CellAt returns the cell at the given x, y position as a *uv.Cell,
// or nil if out of bounds.
func (s *Screen) CellAt(x int, y int) *uv.Cell {
	if y < 0 || y >= s.height || x < 0 || x >= s.width {
		return nil
	}
	c := uvFromCompact(s.rows[y][x], x, s.extras[y], s.styles)
	return &c
}

// SetCell sets the cell at the given x, y position.
func (s *Screen) SetCell(x, y int, c *uv.Cell) {
	if y < 0 || y >= s.height || x < 0 || x >= s.width {
		return
	}
	cell, ex, has := compactFromUV(c, s.styles)
	s.setCompactCell(x, y, cell, ex, has)
}

// Height returns the height of the screen.
func (s *Screen) Height() int {
	return s.height
}

// Width returns the width of the screen.
func (s *Screen) Width() int {
	return s.width
}

// String returns the plain text representation of the screen, matching
// uv.Buffer.String().
func (s *Screen) String() string {
	var buf strings.Builder
	var pending strings.Builder
	for y := range s.height {
		pending.Reset()
		row := s.rows[y]
		ex := s.extras[y]
		for x, c := range row {
			if c.IsZero() {
				continue
			}
			if c.IsEmpty() {
				pending.WriteByte(' ')
				continue
			}
			if pending.Len() > 0 {
				buf.WriteString(pending.String())
				pending.Reset()
			}
			if c.HasGrapheme() && ex != nil {
				buf.WriteString(ex[x].grapheme)
			} else if c.Rune != 0 {
				buf.WriteRune(c.Rune)
			}
		}
		if y < s.height-1 {
			buf.WriteByte('\n')
		}
	}
	return buf.String()
}

// Render returns the screen rendered with ANSI SGR styles and OSC 8 hyperlinks,
// matching uv.Buffer.Render().
func (s *Screen) Render() string {
	lines := make(uv.Lines, s.height)
	for y := range s.height {
		row := s.rows[y]
		ex := s.extras[y]
		line := make(uv.Line, s.width)
		for x, c := range row {
			line[x] = uvFromCompact(c, x, ex, s.styles)
		}
		lines[y] = line
	}
	return lines.Render()
}

// Resize resizes the screen, cropping or extending rows without rewrapping.
func (s *Screen) Resize(width int, height int) {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	oldWidth, oldHeight := s.width, s.height
	if width != oldWidth || height != oldHeight {
		newRows := make([][]Cell, height)
		copyRows := min(oldHeight, height)
		for y := range copyRows {
			oldRow := s.rows[y]
			if width == len(oldRow) {
				newRows[y] = oldRow
			} else {
				nr := newCompactRow(width)
				copy(nr, oldRow[:min(len(oldRow), width)])
				newRows[y] = nr
			}
		}
		for y := copyRows; y < height; y++ {
			newRows[y] = newCompactRow(width)
		}
		s.rows = newRows
		s.width = width
		s.height = height

		// Prune extras for dropped rows/columns.
		if len(s.extras) > 0 {
			for y, re := range s.extras {
				if y >= height {
					delete(s.extras, y)
					continue
				}
				if width < oldWidth {
					for x := range re {
						if x >= width {
							delete(re, x)
						}
					}
					if len(re) == 0 {
						delete(s.extras, y)
					}
				}
			}
		}

		s.rowRev = make([]uint64, height)
		s.bumpAllRows()
	}

	s.flags = resizeFlags(s.flags, height)
	if width != oldWidth {
		// Cropped or padded rows no longer end where they wrapped.
		for y := range s.flags {
			s.flags[y] &^= FlagWrapped
		}
	}
	s.scroll = s.Bounds()
	s.cur.Position = clampPos(s.cur.Position, width, height)
	s.saved.Position = clampPos(s.saved.Position, width, height)
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
	prev := s.flags[y]
	if on {
		s.flags[y] |= flag
	} else {
		s.flags[y] &^= flag
	}
	if s.flags[y] != prev && flag&FlagWrapped != 0 {
		s.bumpRow(y)
	}
}

// clearFlags forgets the wraps of rows erased through their last column,
// which end where they end now, and the prompts of rows erased whole.
func (s *Screen) clearFlags(area uv.Rectangle) {
	if area.Max.X < s.width {
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

// Clear clears the screen with blank cells.
func (s *Screen) Clear() {
	s.ClearArea(s.Bounds())
}

// ClearWithScrollback saves all non-empty lines to scrollback before clearing.
// This is used for operations like ED 2 (erase screen) where content should
// be preserved in history.
func (s *Screen) ClearWithScrollback() {
	if s.scrollback != nil {
		// Save all lines that have content before clearing.
		for y := range s.height {
			row := s.rows[y]
			if len(row) > 0 && !isCompactRowEmpty(row) {
				s.scrollback.pushCompact(row, s.extras[y], s.LineFlags(y))
			}
		}
	}
	s.Clear()
}

// ClearArea clears the given area with EmptyCell.
func (s *Screen) ClearArea(area uv.Rectangle) {
	s.fillCompactArea(EmptyCell, cellExtra{}, false, area)
}

// Fill fills the screen with the given cell.
func (s *Screen) Fill(c *uv.Cell) {
	s.FillArea(c, s.Bounds())
}

// FillArea fills the given area with the given cell.
func (s *Screen) FillArea(c *uv.Cell, area uv.Rectangle) {
	cell, ex, has := compactFromUV(c, s.styles)
	s.fillCompactArea(cell, ex, has, area)
}

// fillBlankArea fills area with the current cursor blank cell (0 allocs).
func (s *Screen) fillBlankArea(area uv.Rectangle) {
	s.fillCompactArea(s.blankCompactCell(), cellExtra{}, false, area)
}

func (s *Screen) fillCompactArea(cell Cell, ex cellExtra, hasExtra bool, area uv.Rectangle) {
	minY := max(0, area.Min.Y)
	maxY := min(s.height, area.Max.Y)
	minX := max(0, area.Min.X)
	maxX := min(s.width, area.Max.X)
	if minY >= maxY || minX >= maxX {
		s.clearFlags(area)
		return
	}

	cw := max(1, cell.Width())
	if cw == 1 && !hasExtra {
		fullRow := minX == 0 && maxX == s.width
		for y := minY; y < maxY; y++ {
			row := s.rows[y]
			if !fullRow {
				// Clean wide chars straddling the fill boundaries.
				if row[minX].Width() == 0 && row[minX].IsZero() {
					s.clearWideOverlap(y, minX)
				}
				if row[maxX-1].Width() > 1 {
					s.clearWideOverlap(y, maxX-1)
				}
			}
			for x := minX; x < maxX; x++ {
				row[x] = cell
			}
			s.clearRowExtrasRange(y, minX, maxX)
			s.bumpRow(y)
		}
		s.clearFlags(area)
		return
	}

	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x += cw {
			s.setCompactCell(x, y, cell, ex, hasExtra)
		}
	}
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
	if s.width <= 0 || s.height <= 0 {
		s.cur.X, s.cur.Y = 0, 0
		return
	}
	old := s.cur.Position
	if !margins {
		y = ordered.Clamp(y, 0, s.height-1)
		x = ordered.Clamp(x, 0, s.width-1)
	} else {
		y = ordered.Clamp(s.scroll.Min.Y+y, s.scroll.Min.Y, s.scroll.Max.Y-1)
		x = ordered.Clamp(s.scroll.Min.X+x, s.scroll.Min.X, s.scroll.Max.X-1)
	}
	s.cur.X, s.cur.Y = x, y

	if s.cb != nil && s.cb.CursorPosition != nil && (old.X != x || old.Y != y) {
		s.cb.CursorPosition(old, uv.Pos(x, y))
	}
}

// moveCursor moves the cursor by the given x and y deltas. If the cursor
// position is inside the scroll region, it is bounded by the scroll region.
// Otherwise, it is bounded by the screen bounds.
// This follows how [ansi.CUU], [ansi.CUD], [ansi.CUF], [ansi.CUB], [ansi.CNL],
// [ansi.CPL].
func (s *Screen) moveCursor(dx, dy int) {
	if s.width <= 0 || s.height <= 0 {
		s.cur.X, s.cur.Y = 0, 0
		return
	}
	scroll := s.scroll
	old := s.cur.Position
	if old.X < scroll.Min.X {
		scroll.Min.X = 0
	}
	if old.X >= scroll.Max.X {
		scroll.Max.X = s.width
	}

	pt := uv.Pos(s.cur.X+dx, s.cur.Y+dy)

	var x, y int
	if old.In(scroll) {
		y = ordered.Clamp(pt.Y, scroll.Min.Y, scroll.Max.Y-1)
		x = ordered.Clamp(pt.X, scroll.Min.X, scroll.Max.X-1)
	} else {
		y = ordered.Clamp(pt.Y, 0, s.height-1)
		x = ordered.Clamp(pt.X, 0, s.width-1)
	}

	s.cur.X, s.cur.Y = x, y

	if s.cb != nil && s.cb.CursorPosition != nil && (old.X != x || old.Y != y) {
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
	s.cur.Position = clampPos(s.cur.Position, s.width, s.height)
	s.syncCursorStyle()

	if s.cb != nil && s.cb.CursorPosition != nil && (old.X != s.cur.X || old.Y != s.cur.Y) {
		s.cb.CursorPosition(old, s.cur.Position)
	}
}

// setCursorHidden sets the cursor hidden.
func (s *Screen) setCursorHidden(hidden bool) {
	changed := s.cur.Hidden != hidden
	s.cur.Hidden = hidden
	if changed && s.cb != nil && s.cb.CursorVisibility != nil {
		s.cb.CursorVisibility(!hidden)
	}
}

func clampPos(pos uv.Position, width, height int) uv.Position {
	if width > 0 {
		pos.X = min(max(pos.X, 0), width-1)
	} else {
		pos.X = 0
	}
	if height > 0 {
		pos.Y = min(max(pos.Y, 0), height-1)
	} else {
		pos.Y = 0
	}
	return pos
}

// setCursorStyle sets the cursor style.
func (s *Screen) setCursorStyle(style CursorStyle, blink bool) {
	changed := s.cur.Style != style || s.cur.Steady != !blink
	s.cur.Style = style
	s.cur.Steady = !blink
	if changed && s.cb != nil && s.cb.CursorStyle != nil {
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
	s.insertCellArea(s.cur.X, s.cur.Y, n, s.blankCompactCell(), s.scroll)
}

func (s *Screen) insertCellArea(x, y, n int, blank Cell, area uv.Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= s.height ||
		x < area.Min.X || x >= area.Max.X || x >= s.width {
		return
	}
	if x+n > area.Max.X {
		n = area.Max.X - x
	}
	row := s.rows[y]
	for i := area.Max.X - 1; i >= x+n && i-n >= area.Min.X; i-- {
		row[i] = row[i-n]
	}
	if len(s.extras) > 0 {
		if re := s.extras[y]; re != nil {
			shifted := make(rowExtra, len(re))
			for col, ex := range re {
				switch {
				case col < x || col >= area.Max.X:
					shifted[col] = ex
				case col+n < area.Max.X:
					shifted[col+n] = ex
				}
			}
			if len(shifted) == 0 {
				delete(s.extras, y)
			} else {
				s.extras[y] = shifted
			}
		}
	}
	for i := x; i < x+n && i < area.Max.X; i++ {
		s.setCompactCell(i, y, blank, cellExtra{}, false)
	}
	s.bumpRow(y)
}

// DeleteCell deletes n cells at the cursor position moving cells to the left.
// This has no effect if the cursor is outside the scroll region.
func (s *Screen) DeleteCell(n int) {
	if n <= 0 {
		return
	}
	s.deleteCellArea(s.cur.X, s.cur.Y, n, s.blankCompactCell(), s.scroll)
}

func (s *Screen) deleteCellArea(x, y, n int, blank Cell, area uv.Rectangle) {
	if n <= 0 || y < area.Min.Y || y >= area.Max.Y || y >= s.height ||
		x < area.Min.X || x >= area.Max.X || x >= s.width {
		return
	}
	remaining := area.Max.X - x
	if n > remaining {
		n = remaining
	}
	row := s.rows[y]
	for i := x; i < area.Max.X-n; i++ {
		srcX := i + n
		srcCell := row[srcX]
		var ex cellExtra
		hasEx := false
		if srcCell.HasExtra() && len(s.extras) > 0 {
			if re := s.extras[y]; re != nil {
				ex, hasEx = re[srcX]
			}
		}
		s.setCompactCell(i, y, srcCell, ex, hasEx)
	}
	for i := area.Max.X - n; i < area.Max.X; i++ {
		s.setCompactCell(i, y, blank, cellExtra{}, false)
	}
	s.bumpRow(y)
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

	// Only operate if cursor Y is within scroll region.
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

	// Only operate if cursor Y is within scroll region.
	if y < scroll.Min.Y || y >= scroll.Max.Y ||
		x < scroll.Min.X || x >= scroll.Max.X {
		return false
	}

	// Save lines to scrollback if we're at the top of the scroll region
	// and the scroll region uses the full width (typical terminal scroll).
	if s.scrollback != nil && y == scroll.Min.Y &&
		scroll.Min.X == 0 && scroll.Max.X == s.width {
		linesToSave := min(n, scroll.Max.Y-y)
		s.scrollback.pushScreenLines(s, y, linesToSave)
	}

	// Track monotonic full-screen scroll-up count for Wave 2 delta frames.
	if y == 0 && scroll.Min.Y == 0 && scroll.Max.Y == s.height &&
		scroll.Min.X == 0 && scroll.Max.X == s.width {
		s.scrollCount += uint64(min(n, s.height))
	}

	s.moveLines(y, -n, s.scroll)
	s.shiftFlags(y, -n)

	return true
}

// blankCompactCell returns a blank cell carrying the current pen background
// color (or EmptyCell when Pen.Bg is nil).
func (s *Screen) blankCompactCell() Cell {
	if s.cur.Pen.Bg == nil {
		return EmptyCell
	}
	bg := ColorFromStd(s.cur.Pen.Bg)
	if s.curBlankStyleID == 0 || s.styles.Get(s.curBlankStyleID).Bg != bg {
		s.curBlankStyleID = s.styles.Intern(Style{Bg: bg})
	}
	return Cell{Rune: ' ', StyleID: s.curBlankStyleID, Flags: cellWidth1}
}

// moveLines inserts (n > 0) or deletes (n < 0) |n| blank lines at row y of
// area. When the area spans the full width, it rotates row slices and rowRev
// in place without copying cells or allocating.
func (s *Screen) moveLines(y, n int, area uv.Rectangle) {
	if n == 0 || y < area.Min.Y || y >= area.Max.Y || y >= s.height {
		return
	}
	if area.Min.X != 0 || area.Max.X != s.width {
		s.moveLinesPartial(y, n, area)
		return
	}

	maxY := min(area.Max.Y, s.height)
	rows := s.rows[y:maxY]
	revs := s.rowRev[y:maxY]
	k := min(max(n, -n), len(rows))

	// Rotate right by k to insert, left by k to delete, without allocating.
	split := len(rows) - k
	if n < 0 {
		split = k
	}
	slices.Reverse(rows[:split])
	slices.Reverse(rows[split:])
	slices.Reverse(rows)

	slices.Reverse(revs[:split])
	slices.Reverse(revs[split:])
	slices.Reverse(revs)

	// Shift sparse row extras in [y, maxY) if any exist.
	if len(s.extras) > 0 {
		shifted := make(map[int]rowExtra, len(s.extras))
		for ry, re := range s.extras {
			if ry < y || ry >= maxY {
				shifted[ry] = re
				continue
			}
			var dst int
			if n > 0 {
				dst = ry + k
				if dst >= maxY {
					continue // pushed out bottom
				}
			} else {
				dst = ry - k
				if dst < y {
					continue // pushed out top
				}
			}
			shifted[dst] = re
		}
		if len(shifted) == 0 {
			s.extras = nil
		} else {
			s.extras = shifted
		}
	}

	freshStart := y
	if n < 0 {
		freshStart = maxY - k
	}
	blank := s.blankCompactCell()
	for r := freshStart; r < freshStart+k; r++ {
		row := s.rows[r]
		for x := range row {
			row[x] = blank
		}
		s.bumpRow(r)
	}
}

func (s *Screen) moveLinesPartial(y, n int, area uv.Rectangle) {
	maxY := min(area.Max.Y, s.height)
	minX := max(0, area.Min.X)
	maxX := min(s.width, area.Max.X)
	if minX >= maxX {
		return
	}
	blank := s.blankCompactCell()
	if n > 0 {
		if y+n > maxY {
			n = maxY - y
		}
		for dst := maxY - 1; dst >= y+n; dst-- {
			src := dst - n
			copy(s.rows[dst][minX:maxX], s.rows[src][minX:maxX])
			s.copyExtrasPartial(src, dst, minX, maxX)
			s.bumpRow(dst)
		}
		for r := y; r < y+n; r++ {
			for x := minX; x < maxX; x++ {
				s.setCompactCell(x, r, blank, cellExtra{}, false)
			}
			s.bumpRow(r)
		}
	} else {
		k := min(-n, maxY-y)
		for dst := y; dst < maxY-k; dst++ {
			src := dst + k
			copy(s.rows[dst][minX:maxX], s.rows[src][minX:maxX])
			s.copyExtrasPartial(src, dst, minX, maxX)
			s.bumpRow(dst)
		}
		for r := maxY - k; r < maxY; r++ {
			for x := minX; x < maxX; x++ {
				s.setCompactCell(x, r, blank, cellExtra{}, false)
			}
			s.bumpRow(r)
		}
	}
}

func (s *Screen) copyExtrasPartial(srcY, dstY, minX, maxX int) {
	if len(s.extras) == 0 {
		return
	}
	s.clearRowExtrasRange(dstY, minX, maxX)
	srcRe := s.extras[srcY]
	if srcRe == nil {
		return
	}
	for x, ex := range srcRe {
		if x >= minX && x < maxX {
			s.setCellExtra(dstY, x, ex)
		}
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
	if s.scroll.Min.X != 0 || s.scroll.Max.X != s.width {
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
	if sb != nil && sb.styles == nil {
		sb.styles = s.styles
	}
}

// SetScrollbackSize sets the maximum number of lines in the scrollback buffer.
func (s *Screen) SetScrollbackSize(maxLines int) {
	if s.scrollback == nil {
		s.scrollback = newScrollback(maxLines, s.styles)
	} else {
		s.scrollback.SetMaxLines(maxLines)
	}
}

// SetScrollbackMaxBytes sets the hard memory budget in bytes for the
// scrollback buffer (0 means unlimited).
func (s *Screen) SetScrollbackMaxBytes(maxBytes int) {
	if s.scrollback == nil {
		s.scrollback = newScrollback(DefaultScrollbackSize, s.styles)
	}
	s.scrollback.SetMaxBytes(maxBytes)
}

// markUsedStyles marks all StyleIDs referenced by this screen's cells and cursor.
func (s *Screen) markUsedStyles(used []bool) {
	for _, row := range s.rows {
		for _, c := range row {
			if id := c.StyleID; id != 0 && int(id) < len(used) {
				used[id] = true
			}
		}
	}
	if s.curStyleID != 0 && int(s.curStyleID) < len(used) {
		used[s.curStyleID] = true
	}
	if s.curBlankStyleID != 0 && int(s.curBlankStyleID) < len(used) {
		used[s.curBlankStyleID] = true
	}
	if s.scrollback != nil {
		s.scrollback.markUsedStyles(used)
	}
}

// remapStyles updates all StyleIDs in this screen according to remap.
func (s *Screen) remapStyles(remap []uint32) {
	for _, row := range s.rows {
		for x := range row {
			if id := row[x].StyleID; id != 0 && int(id) < len(remap) {
				row[x].StyleID = remap[id]
			}
		}
	}
	if s.curStyleID != 0 && int(s.curStyleID) < len(remap) {
		s.curStyleID = remap[s.curStyleID]
	}
	if s.curBlankStyleID != 0 && int(s.curBlankStyleID) < len(remap) {
		s.curBlankStyleID = remap[s.curBlankStyleID]
	}
	if s.scrollback != nil {
		s.scrollback.remapStyles(remap)
	}
}
