package vt

import (
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
)

// DefaultScrollbackSize is the default maximum number of lines in the scrollback buffer.
const DefaultScrollbackSize = 10000

const (
	// scrollPageCells is the default cell capacity of one scrollback arena page.
	// 4096 * 12 bytes = 48 KiB of pointer-free Cell storage (GC noscan).
	scrollPageCells = 4096
	// maxRecycledPages bounds how many empty pages we keep for zero-alloc
	// steady-state scrolling while freeing excess pages on shrink/clear.
	maxRecycledPages = 4

	cellBytes  = int(unsafe.Sizeof(Cell{}))
	sbRowBytes = int(unsafe.Sizeof(sbRow{}))
)

func rowExtraBytes(ex rowExtra) int {
	if len(ex) == 0 {
		return 0
	}
	// Account for the sparse map header/bucket overhead plus per-entry string storage.
	n := 48
	for _, ce := range ex {
		n += 64 + len(ce.grapheme) + len(ce.link.URL) + len(ce.link.Params)
	}
	return n
}

func scrollbackRowBytes(cellCount int, ex rowExtra) int {
	return sbRowBytes + cellCount*cellBytes + rowExtraBytes(ex)
}

// scrollPage is a contiguous pointer-free arena of compact Cells backing
// multiple scrollback rows.
type scrollPage struct {
	cells []Cell
	used  int
	refs  int
}

// sbRow is a pointer-free ring slot describing one scrollback line.
// pageIdx is 1-based into Scrollback.pages; 0 denotes an empty (0-cell) line.
type sbRow struct {
	pageIdx uint32
	start   uint32
	length  uint32
	flags   LineFlags
}

// Scrollback represents a ring-buffered compact scrollback store for lines
// scrolled off the main screen.
type Scrollback struct {
	ring        []sbRow
	head        int // index of oldest line in ring
	count       int // number of live lines in ring
	maxLines    int
	maxBytes    int // hard byte cap across live rows; 0 = unlimited
	liveBytes   int
	pages       []*scrollPage
	freePages   []uint32 // 1-based page indices whose refs == 0
	tailPage    uint32   // 1-based index of page currently receiving appends (0 = none)
	extras      map[int]rowExtra
	styles      *StyleTable
	totalPushed uint64
	epoch       uint64
}

// NewScrollback creates a new scrollback buffer with the given maximum number of lines.
func NewScrollback(maxLines int) *Scrollback {
	return newScrollback(maxLines, nil)
}

func newScrollback(maxLines int, styles *StyleTable) *Scrollback {
	if maxLines <= 0 {
		maxLines = DefaultScrollbackSize
	}
	if styles == nil {
		styles = &StyleTable{}
	}
	return &Scrollback{
		maxLines: maxLines,
		styles:   styles,
		epoch:    1,
	}
}

// slotIndex maps logical index i (0 = oldest, count-1 = newest) to its ring slot.
func (s *Scrollback) slotIndex(i int) int {
	idx := s.head + i
	if idx >= len(s.ring) {
		idx -= len(s.ring)
	}
	return idx
}

// ensureRingCapacity grows the ring slice lazily without reallocating on every
// SetMaxLines call (for example when trackHistory temporarily doubles maxLines).
func (s *Scrollback) ensureRingCapacity(needed int) {
	if needed <= len(s.ring) {
		return
	}
	newCap := len(s.ring)
	if newCap == 0 {
		newCap = min(s.maxLines, 256)
	}
	for newCap < needed {
		newCap *= 2
	}
	if newCap > s.maxLines {
		newCap = s.maxLines
	}
	if newCap < needed {
		newCap = needed
	}
	newRing := make([]sbRow, newCap)
	var newExtras map[int]rowExtra
	if len(s.extras) > 0 {
		newExtras = make(map[int]rowExtra, len(s.extras))
	}
	for i := range s.count {
		oldSlot := s.slotIndex(i)
		newRing[i] = s.ring[oldSlot]
		if newExtras != nil {
			if ex, ok := s.extras[oldSlot]; ok {
				newExtras[i] = ex
			}
		}
	}
	clear(s.ring)
	s.ring = newRing
	s.head = 0
	s.extras = newExtras
}

func (s *Scrollback) pageCapacity(minCells int) int {
	capCells := min(scrollPageCells, max(256, s.maxLines*64))
	if minCells > capCells {
		return minCells
	}
	return capCells
}

// allocCells reserves n contiguous cells from the tail page (recycling an
// evicted page when available) and returns (pageIdx, start, destSlice).
func (s *Scrollback) allocCells(n int) (uint32, uint32, []Cell) {
	if n <= 0 {
		return 0, 0, nil
	}
	if s.tailPage != 0 {
		p := s.pages[s.tailPage-1]
		if p != nil && len(p.cells)-p.used >= n {
			start := p.used
			p.used += n
			p.refs++
			return s.tailPage, uint32(start), p.cells[start : start+n]
		}
	}

	// Acquire a recycled page or allocate a new one.
	var pageIdx uint32
	for len(s.freePages) > 0 {
		last := len(s.freePages) - 1
		idx := s.freePages[last]
		s.freePages = s.freePages[:last]
		p := s.pages[idx-1]
		if p != nil && len(p.cells) >= n {
			p.used = n
			p.refs = 1
			s.tailPage = idx
			return idx, 0, p.cells[:n]
		}
		if p == nil {
			pageIdx = idx
			break
		}
		// Recycled page is too small for this oversized line; release it and reuse its slot.
		s.pages[idx-1] = nil
		pageIdx = idx
		break
	}

	p := &scrollPage{
		cells: make([]Cell, s.pageCapacity(n)),
		used:  n,
		refs:  1,
	}
	if pageIdx != 0 {
		s.pages[pageIdx-1] = p
	} else {
		s.pages = append(s.pages, p)
		pageIdx = uint32(len(s.pages))
	}
	s.tailPage = pageIdx
	return pageIdx, 0, p.cells[:n]
}

// releaseSlot releases the cell page reference and side-table entry for ring slot.
func (s *Scrollback) releaseSlot(slot int) {
	row := s.ring[slot]
	s.liveBytes = max(0, s.liveBytes-scrollbackRowBytes(int(row.length), s.extras[slot]))
	s.ring[slot] = sbRow{}
	if len(s.extras) > 0 {
		delete(s.extras, slot)
	}
	if row.pageIdx == 0 {
		return
	}
	p := s.pages[row.pageIdx-1]
	if p == nil {
		return
	}
	p.refs--
	if p.refs == 0 {
		if s.tailPage == row.pageIdx {
			// Active tail page became completely empty: reset it in place!
			p.used = 0
			return
		}
		// Count live allocated recycled pages to decide whether to recycle or free.
		recycled := 0
		for _, idx := range s.freePages {
			if s.pages[idx-1] != nil {
				recycled++
			}
		}
		if recycled < maxRecycledPages && len(p.cells) <= scrollPageCells {
			p.used = 0
		} else {
			s.pages[row.pageIdx-1] = nil
		}
		s.freePages = append(s.freePages, row.pageIdx)
	}
}

func (s *Scrollback) evictOldest() {
	s.releaseSlot(s.head)
	s.head++
	if s.head >= len(s.ring) {
		s.head = 0
	}
	s.count--
}

// pushCompact appends a compact cell row and its optional side-table + flags.
// A wrapped row retains trailing spaces; an unwrapped row trims them.
func (s *Scrollback) pushCompact(row []Cell, ex rowExtra, flags LineFlags) {
	if s == nil || s.maxLines <= 0 {
		return
	}
	end := len(row)
	if flags&FlagWrapped == 0 {
		end = len(trimCompactRow(row))
	}

	var cloned rowExtra
	if end > 0 {
		cloned = cloneRowExtra(ex, end)
	}
	needed := scrollbackRowBytes(end, cloned)
	if s.maxBytes > 0 && needed > s.maxBytes {
		for s.count > 0 {
			s.evictOldest()
		}
		s.totalPushed++
		return
	}

	for s.count >= s.maxLines || (s.maxBytes > 0 && s.count > 0 && s.liveBytes+needed > s.maxBytes) {
		// Evict oldest slot before allocating so a full ring recycles pages at 0 allocs.
		s.evictOldest()
	}

	s.ensureRingCapacity(s.count + 1)
	slot := s.slotIndex(s.count)
	s.count++
	s.totalPushed++

	if end == 0 {
		s.ring[slot] = sbRow{flags: flags}
		s.liveBytes += needed
		return
	}

	pageIdx, start, dst := s.allocCells(end)
	copy(dst, row[:end])
	s.ring[slot] = sbRow{
		pageIdx: pageIdx,
		start:   start,
		length:  uint32(end),
		flags:   flags,
	}
	if cloned != nil {
		if s.extras == nil {
			s.extras = make(map[int]rowExtra)
		}
		s.extras[slot] = cloned
	}
	s.liveBytes += needed
}

// Push adds a uv.Line without flags to the scrollback buffer.
// If the buffer is full, the oldest line is removed.
func (s *Scrollback) Push(line uv.Line) {
	s.PushLine(line, 0)
}

// PushLine adds a uv.Line with its flags. A wrapped line keeps every cell,
// trailing blanks included, because they are part of the text that continues;
// other lines lose trailing blanks.
func (s *Scrollback) PushLine(line uv.Line, flags LineFlags) {
	if s == nil || s.maxLines <= 0 {
		return
	}
	end := len(line)
	if flags&FlagWrapped == 0 {
		end = 0
		for i := len(line) - 1; i >= 0; i-- {
			c := &line[i]
			if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
				end = i + 1
				break
			}
		}
	}
	if end == 0 {
		s.pushCompact(nil, nil, flags)
		return
	}
	compact := make([]Cell, end)
	var ex rowExtra
	for i := range end {
		cell, ce, has := compactFromUV(&line[i], s.styles)
		compact[i] = cell
		if has {
			if ex == nil {
				ex = make(rowExtra)
			}
			ex[i] = ce
		}
	}
	s.pushCompact(compact, ex, flags)
}

// pushScreenLines pushes n lines from screen s starting at row y into scrollback.
func (s *Scrollback) pushScreenLines(scr *Screen, y, n int) {
	if s == nil || scr == nil || n <= 0 {
		return
	}
	limit := min(n, scr.height-y)
	for i := range limit {
		rowY := y + i
		var f LineFlags
		if rowY < len(scr.flags) {
			f = scr.flags[rowY]
		}
		s.pushCompact(scr.rows[rowY], scr.extras[rowY], f)
	}
}

// Len returns the number of lines in the scrollback buffer.
func (s *Scrollback) Len() int {
	if s == nil {
		return 0
	}
	return s.count
}

// MaxLines returns the maximum number of lines the scrollback buffer can hold.
func (s *Scrollback) MaxLines() int {
	if s == nil {
		return 0
	}
	return s.maxLines
}

// TotalPushed returns the monotonic total number of lines ever pushed into
// this scrollback buffer, even after eviction wraps the ring.
func (s *Scrollback) TotalPushed() uint64 {
	if s == nil {
		return 0
	}
	return s.totalPushed
}

// Epoch returns a monotonic counter incremented when scrollback history is
// cleared or rewritten (for example by reflow).
func (s *Scrollback) Epoch() uint64 {
	if s == nil {
		return 0
	}
	return s.epoch
}

// Bytes returns the tracked memory footprint in bytes of live rows in the
// scrollback buffer.
func (s *Scrollback) Bytes() int {
	if s == nil {
		return 0
	}
	return s.liveBytes
}

// MaxBytes returns the hard byte budget for live scrollback rows (0 = unlimited).
func (s *Scrollback) MaxBytes() int {
	if s == nil {
		return 0
	}
	return s.maxBytes
}

// SetMaxLines sets the maximum number of lines in the scrollback buffer.
// If the current number of lines exceeds the new maximum, oldest lines are
// evicted and their slots are cleared.
func (s *Scrollback) SetMaxLines(maxLines int) {
	if s == nil || maxLines <= 0 {
		return
	}
	s.maxLines = maxLines
	for s.count > maxLines {
		s.evictOldest()
	}
}

// SetMaxBytes sets the hard byte budget for live scrollback rows (0 = unlimited).
// If the current byte usage exceeds the new limit, oldest lines are evicted.
func (s *Scrollback) SetMaxBytes(maxBytes int) {
	if s == nil {
		return
	}
	if maxBytes < 0 {
		maxBytes = 0
	}
	s.maxBytes = maxBytes
	if maxBytes > 0 {
		for s.count > 0 && s.liveBytes > maxBytes {
			s.evictOldest()
		}
	}
}

// RowCells returns a zero-alloc read-only slice of compact Cells for
// scrollback line index (0 = oldest, Len()-1 = most recent).
// Returns nil if index is out of bounds or the line is empty.
func (s *Scrollback) RowCells(index int) []Cell {
	if s == nil || index < 0 || index >= s.count {
		return nil
	}
	row := s.ring[s.slotIndex(index)]
	if row.pageIdx == 0 || row.length == 0 {
		return nil
	}
	p := s.pages[row.pageIdx-1]
	return p.cells[row.start : row.start+row.length]
}

// rowExtraAt returns the sparse side-table for scrollback line index, or nil.
func (s *Scrollback) rowExtraAt(index int) rowExtra {
	if s == nil || len(s.extras) == 0 || index < 0 || index >= s.count {
		return nil
	}
	return s.extras[s.slotIndex(index)]
}

// CellGrapheme returns the multi-rune grapheme cluster string at (x, y) in
// scrollback (y = 0 is oldest), or "" if none.
func (s *Scrollback) CellGrapheme(x, y int) string {
	ex := s.rowExtraAt(y)
	if ex == nil {
		return ""
	}
	return ex[x].grapheme
}

// CellLink returns the OSC 8 hyperlink at (x, y) in scrollback (y = 0 is
// oldest), or the zero uv.Link if none.
func (s *Scrollback) CellLink(x, y int) uv.Link {
	ex := s.rowExtraAt(y)
	if ex == nil {
		return uv.Link{}
	}
	return ex[x].link
}

// Line returns a reconstructed uv.Line at the given index (0 = oldest).
// Returns nil if index is out of bounds.
func (s *Scrollback) Line(index int) uv.Line {
	if s == nil || index < 0 || index >= s.count {
		return nil
	}
	cells := s.RowCells(index)
	if len(cells) == 0 {
		return uv.Line{}
	}
	ex := s.rowExtraAt(index)
	out := make(uv.Line, len(cells))
	for x, c := range cells {
		out[x] = uvFromCompact(c, x, ex, s.styles)
	}
	return out
}

// LineFlags returns the flags of the line at index.
func (s *Scrollback) LineFlags(index int) LineFlags {
	if s == nil || index < 0 || index >= s.count {
		return 0
	}
	return s.ring[s.slotIndex(index)].flags
}

// Wrapped reports whether the line at index continues onto the next one.
func (s *Scrollback) Wrapped(index int) bool {
	return s.LineFlags(index)&FlagWrapped != 0
}

// Lines returns all lines in the scrollback buffer (0 = oldest) as uv.Lines.
func (s *Scrollback) Lines() []uv.Line {
	if s == nil {
		return nil
	}
	out := make([]uv.Line, s.count)
	for i := range s.count {
		out[i] = s.Line(i)
	}
	return out
}

// Clear removes all lines from the scrollback buffer and releases all backing
// cell pages and side-table entries so evicted history is never pinned.
func (s *Scrollback) Clear() {
	if s == nil {
		return
	}
	clear(s.ring)
	s.head = 0
	s.count = 0
	s.liveBytes = 0
	clear(s.pages)
	s.pages = nil
	s.freePages = nil
	s.tailPage = 0
	s.extras = nil
	s.epoch++
}

// replaceCompact swaps in a whole new compact history, newest last, trimmed
// to maxLines and packed into fresh pages with zero backing-array pinning.
// When bumpEpoch is true (width reflow or rows pulled back off scrollback),
// epoch increments; otherwise (height-only resize appending rows in order)
// epoch stays unchanged and totalPushed advances by the exact net lines added.
func (s *Scrollback) replaceCompact(rows [][]Cell, extras []rowExtra, flags []LineFlags, bumpEpoch bool) {
	if s == nil {
		return
	}
	savedTotal := s.totalPushed
	savedEpoch := s.epoch
	origLen := s.count
	newTotalRows := len(rows)
	if !bumpEpoch && newTotalRows < origLen {
		bumpEpoch = true
	}
	s.Clear()
	if excess := len(rows) - s.maxLines; excess > 0 {
		rows = rows[excess:]
		if len(extras) > excess {
			extras = extras[excess:]
		} else {
			extras = nil
		}
		if len(flags) > excess {
			flags = flags[excess:]
		} else {
			flags = nil
		}
	}
	for i, row := range rows {
		var ex rowExtra
		if i < len(extras) {
			ex = extras[i]
		}
		var f LineFlags
		if i < len(flags) {
			f = flags[i]
		}
		s.pushCompact(row, ex, f)
	}
	if !bumpEpoch && s.count < origLen {
		bumpEpoch = true
	}
	if bumpEpoch {
		s.epoch = savedEpoch + 1
		if savedTotal > s.totalPushed {
			s.totalPushed = savedTotal
		}
	} else {
		s.epoch = savedEpoch
		s.totalPushed = savedTotal + uint64(newTotalRows-origLen)
	}
}

// CellAt returns the cell at (x, y) in the scrollback buffer (y = 0 is oldest).
// Returns nil if out of bounds.
func (s *Scrollback) CellAt(x, y int) *uv.Cell {
	cells := s.RowCells(y)
	if x < 0 || x >= len(cells) {
		return nil
	}
	c := uvFromCompact(cells[x], x, s.rowExtraAt(y), s.styles)
	return &c
}

// remapStyles updates all StyleIDs in live scrollback pages according to remap.
func (s *Scrollback) remapStyles(remap []uint32) {
	if s == nil {
		return
	}
	for i := range s.count {
		cells := s.RowCells(i)
		for x := range cells {
			if id := cells[x].StyleID; id != 0 && int(id) < len(remap) {
				cells[x].StyleID = remap[id]
			}
		}
	}
}

// markUsedStyles marks all StyleIDs referenced by live scrollback cells.
func (s *Scrollback) markUsedStyles(used []bool) {
	if s == nil {
		return
	}
	for i := range s.count {
		for _, c := range s.RowCells(i) {
			if id := c.StyleID; id != 0 && int(id) < len(used) {
				used[id] = true
			}
		}
	}
}
