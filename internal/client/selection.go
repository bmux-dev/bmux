package client

import (
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

type point struct{ x, y int }
type selection struct {
	start, end       point
	active, dragging bool
	lines            map[int][]protocol.Cell
	wrapped          map[int]bool // rows the terminal wrapped onto the next
	epoch            uint64
	alt, observed    bool
}

func before(a, b point) bool { return a.y < b.y || (a.y == b.y && a.x < b.x) }
func (s *selection) bounds() (point, point) {
	if before(s.end, s.start) {
		return s.end, s.start
	}
	return s.start, s.end
}
func (s *selection) contains(x, y int) bool {
	if !s.active {
		return false
	}
	a, b := s.bounds()
	if row := s.lines[a.y]; a.x >= 0 && a.x < len(row) {
		a.x = cellOwner(row, a.x)
	}
	if row := s.lines[b.y]; b.x >= 0 && b.x < len(row) {
		owner := cellOwner(row, b.x)
		b.x = min(len(row)-1, owner+max(1, row[owner].Width)-1)
	}
	p := point{x, y}
	return !before(p, a) && !before(b, p)
}
func (s *selection) remember(f *protocol.TerminalFrame) {
	if !s.active {
		return
	}
	if f == nil {
		*s = selection{}
		return
	}
	a, b := s.bounds()
	// Base and HistoryStart are absolute row IDs within one screen epoch.
	// Moving a viewport is not eviction; only the retained-history floor is.
	if a.y < f.HistoryStart || (s.observed && (s.epoch != f.Epoch || s.alt != f.Alt)) {
		*s = selection{}
		return
	}
	s.epoch, s.alt, s.observed = f.Epoch, f.Alt, true
	if s.lines == nil {
		s.lines = make(map[int][]protocol.Cell)
		s.wrapped = make(map[int]bool)
	}
	for y, row := range f.Rows {
		id := f.Base + y
		if old, ok := s.lines[id]; ok && id >= a.y && id <= b.y {
			lo, hi := 0, max(len(old), len(row))-1
			if id == a.y {
				lo = a.x
			}
			if id == b.y {
				hi = b.x
			}
			if !sameSelectedCells(old, row, lo, hi) {
				// Never retain cached copy text beneath freshly painted glyphs.
				*s = selection{}
				return
			}
		}
		// Refresh unselected cells too: extending a drag must use the latest row.
		s.lines[id] = append([]protocol.Cell(nil), row...)
		s.wrapped[id] = y < len(f.Wrapped) && f.Wrapped[y]
	}
	for id := range s.lines {
		if id < f.HistoryStart {
			delete(s.lines, id)
			delete(s.wrapped, id)
		}
	}
}

// cellOwner walks back over continuation cells, including graphemes wider than
// two columns. Empty unowned cells are ordinary visible blanks.
func cellOwner(row []protocol.Cell, x int) int {
	owner := x
	for owner > 0 && row[owner].Width == 0 {
		owner--
	}
	if owner+max(1, row[owner].Width) > x {
		return owner
	}
	return x
}

func visibleCellText(c protocol.Cell) string {
	if c.Attrs&uv.AttrConceal != 0 || c.Text == "" {
		return strings.Repeat(" ", max(1, c.Width))
	}
	return c.Text
}

func sameSelectedCells(old, row []protocol.Cell, lo, hi int) bool {
	for x := max(0, lo); x <= hi; x++ {
		if x >= len(old) || x >= len(row) {
			if len(old) != len(row) {
				return false
			}
			break
		}
		a, b := cellOwner(old, x), cellOwner(row, x)
		if a != b || max(1, old[a].Width) != max(1, row[b].Width) || visibleCellText(old[a]) != visibleCellText(row[b]) {
			return false
		}
	}
	return true
}

// text is the selected text. Rows the terminal wrapped join the next row
// without a newline and keep their trailing blanks, which are part of the
// line that continues.
func (s *selection) text() string {
	if !s.active {
		return ""
	}
	a, b := s.bounds()
	var text strings.Builder
	for y := a.y; y <= b.y; y++ {
		row := s.lines[y]
		lo, hi := 0, len(row)-1
		if y == a.y {
			lo = a.x
		}
		if y == b.y {
			hi = b.x
		}
		if lo >= 0 && lo < len(row) {
			lo = cellOwner(row, lo)
		}
		var line strings.Builder
		for x := max(0, lo); x <= hi && x < len(row); x++ {
			c := row[x]
			if cellOwner(row, x) != x {
				continue
			}
			line.WriteString(visibleCellText(c))
		}
		if y < b.y && s.wrapped[y] {
			text.WriteString(line.String())
			continue
		}
		text.WriteString(strings.TrimRight(line.String(), " "))
		if y < b.y {
			text.WriteByte('\n')
		}
	}
	return text.String()
}

func wordBounds(row []protocol.Cell, x int) (int, int) {
	x = max(0, min(x, len(row)-1))
	if len(row) == 0 {
		return 0, 0
	}
	x = cellOwner(row, x)
	class := func(c protocol.Cell) int {
		if strings.TrimSpace(visibleCellText(c)) == "" {
			return 0
		}
		for _, r := range c.Text {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_./~-", r) {
				return 1
			}
		}
		return 2
	}
	kind := class(row[x])
	lo, hi := x, x+max(1, row[x].Width)-1
	for lo > 0 {
		previous := cellOwner(row, lo-1)
		if class(row[previous]) != kind {
			break
		}
		lo = previous
	}
	for hi+1 < len(row) && class(row[hi+1]) == kind {
		hi += max(1, row[hi+1].Width)
	}
	return lo, min(hi, len(row)-1)
}

func fuzzyScore(query, text string) (int, bool) {
	q, t := []rune(strings.ToLower(query)), []rune(strings.ToLower(text))
	if len(q) == 0 {
		return 0, true
	}
	i, score, last := 0, 0, -2
	for pos, r := range t {
		if r != q[i] {
			continue
		}
		score += 10
		if pos == last+1 {
			score += 15
		}
		if pos == 0 || strings.ContainsRune(" /_-.", t[pos-1]) {
			score += 10
		}
		score -= pos
		last, i = pos, i+1
		if i == len(q) {
			return score, true
		}
	}
	return 0, false
}
