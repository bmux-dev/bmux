package server

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Search runs over the pane's whole retained history, one printed line at a
// time: rows the terminal wrapped are searched together, so a wrapped URL is
// found. A lowercase query ignores case; any uppercase letter makes it exact,
// like vi's smartcase. Searches wrap around the ends.

// searchLine is one printed line: its text and, for each byte, the row and
// column of the cell it came from.
type searchLine struct {
	text  string
	cells []cellPos
}

type cellPos struct{ row, col int }

func (l searchLine) at(i int) (row, col int) {
	return l.cells[i].row, l.cells[i].col
}

// logicalLinesLocked groups retained rows into printed lines, as row ranges.
// Caller holds pane.mu.
func (p *pane) logicalLinesLocked() [][2]int {
	history := p.term.ScrollbackLen()
	total := max(p.historyTotal, history)
	first, end := total-history, total+p.term.Height()
	var lines [][2]int
	start := first
	for id := first; id < end; id++ {
		if _, wrapped, _ := p.historyRowLocked(id); !wrapped || id == end-1 {
			lines = append(lines, [2]int{start, id})
			start = id + 1
		}
	}
	return lines
}

// searchLineLocked builds the text of rows from..to, folding case when fold.
func (p *pane) searchLineLocked(from, to int, fold bool) searchLine {
	var line searchLine
	var text strings.Builder
	for id := from; id <= to; id++ {
		row, _, _ := p.historyRowLocked(id)
		for x, c := range row {
			if c.IsZero() {
				continue
			}
			content := c.Content
			if content == "" {
				content = " "
			}
			if fold {
				content = strings.ToLower(content)
			}
			text.WriteString(content)
			for range len(content) {
				line.cells = append(line.cells, cellPos{id, x})
			}
		}
	}
	line.text = text.String()
	return line
}

// searchHistory finds query from the cell at (from.FromX, from.FromY),
// searching up (older) or down (newer), and returns the match's first and
// last cells. The cell the search starts on is never a match, so repeating
// a search moves on.
func (p *pane) searchHistory(from protocol.TextRange, query string, up bool) (protocol.TextRange, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term.IsAltScreen() {
		return protocol.TextRange{}, fmt.Errorf("full-screen programs have no scrollback")
	}
	if from.Epoch != p.epoch {
		return protocol.TextRange{}, fmt.Errorf("the pane's history changed; search again")
	}
	if query == "" {
		return protocol.TextRange{}, fmt.Errorf("nothing to search for")
	}
	fold := !strings.ContainsFunc(query, unicode.IsUpper)
	if fold {
		query = strings.ToLower(query)
	}
	lines := p.logicalLinesLocked()
	if len(lines) == 0 {
		return protocol.TextRange{}, fmt.Errorf("no match for %q", query)
	}
	home := 0
	for i, l := range lines {
		if from.FromY >= l[0] && from.FromY <= l[1] {
			home = i
			break
		}
		if from.FromY > l[1] {
			home = i
		}
	}
	// before reports whether a match at (row, col) comes before the start.
	before := func(row, col int) bool {
		return row < from.FromY || (row == from.FromY && col < from.FromX)
	}
	// Visit every line once from home in the search direction, wrapping,
	// and home again last for matches on the far side of the start.
	for step := 0; step <= len(lines); step++ {
		i := (home + step) % len(lines)
		if up {
			i = (home - step + len(lines)*2) % len(lines)
		}
		line := p.searchLineLocked(lines[i][0], lines[i][1], fold)
		var hits []int
		for offset := 0; ; {
			j := strings.Index(line.text[offset:], query)
			if j < 0 {
				break
			}
			hits = append(hits, offset+j)
			offset += j + 1
		}
		if up {
			for k := len(hits) - 1; k >= 0; k-- {
				row, col := line.at(hits[k])
				if (step == 0 && before(row, col)) || (step > 0 && (i != home || !before(row, col))) {
					return line.match(hits[k], len(query)), nil
				}
			}
			continue
		}
		for _, hit := range hits {
			row, col := line.at(hit)
			after := !before(row, col) && (row != from.FromY || col != from.FromX)
			if (step == 0 && after) || (step > 0 && (i != home || !after)) {
				return line.match(hit, len(query)), nil
			}
		}
	}
	return protocol.TextRange{}, fmt.Errorf("no match for %q", query)
}

// match is the cells of the n bytes at offset.
func (l searchLine) match(offset, n int) protocol.TextRange {
	fromRow, fromCol := l.at(offset)
	toRow, toCol := l.at(offset + n - 1)
	return protocol.TextRange{FromX: fromCol, FromY: fromRow, ToX: toCol, ToY: toRow}
}

// searchHistory searches a pane of the session; pane 0 is the active
// window's focused pane.
func (s *session) searchHistory(id int, from *protocol.TextRange, query, direction string) (protocol.TextRange, error) {
	if from == nil {
		return protocol.TextRange{}, fmt.Errorf("search-history needs a starting point")
	}
	if direction != "up" && direction != "down" {
		return protocol.TextRange{}, fmt.Errorf("search direction must be up or down")
	}
	p, err := s.livePane(id)
	if err != nil {
		return protocol.TextRange{}, err
	}
	match, err := p.searchHistory(*from, query, direction == "up")
	match.Epoch = from.Epoch
	return match, err
}
