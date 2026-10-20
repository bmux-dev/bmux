package server

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Give x/vt a bounded scratch margin during each output operation, then trim
// back to the retained limit (scrollback.lines, read on every operation so a
// reload reaches running panes at their next output). We can observe newly
// added lines even when the retained ring is full, without parsing
// ANSI/Unicode ourselves or modifying cells. If an operation replaces more
// than the entire ring, or a smaller limit trims it, old anchors are
// genuinely gone; explicitly start a new coordinate epoch instead of reusing
// their IDs. Caller holds pane.mu.
func (p *pane) trackHistory(operation func()) {
	limit := currentSettings().scrollback
	before := p.term.ScrollbackLen()
	p.term.SetScrollbackSize(limit*2 + 1)
	operation()
	after := p.term.ScrollbackLen()
	if after < before || after >= limit*2+1 {
		p.epoch++
		p.historyTotal = min(after, limit)
	} else {
		p.historyTotal += after - before
	}
	p.term.SetScrollbackSize(limit)
}

// historyRowLocked returns a retained row by absolute ID (as frames number
// them) and whether the terminal wrapped it onto the next. Caller holds
// pane.mu.
func (p *pane) historyRowLocked(id int) (row uv.Line, wrapped, ok bool) {
	history := p.term.ScrollbackLen()
	total := max(p.historyTotal, history)
	start := total - history
	switch {
	case id < start:
		return nil, false, false
	case id < total:
		return p.term.Scrollback().Line(id - start), p.term.ScrollbackWrapped(id - start), true
	case id-total < p.term.Height():
		y := id - total
		row = make(uv.Line, p.term.Width())
		for x := range row {
			if c := p.term.CellAt(x, y); c != nil {
				row[x] = *c
			}
		}
		return row, p.term.LineWrapped(y), true
	}
	return nil, false, false
}

// historyText returns the text in a range of retained history, for keyboard
// selections that may reach rows the client never saw. Rows the terminal
// wrapped join the next without a newline in char and line selections;
// block selections take the same columns of every row.
func (p *pane) historyText(r protocol.TextRange) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term.IsAltScreen() {
		return "", fmt.Errorf("full-screen programs have no scrollback")
	}
	if r.Epoch != p.epoch {
		return "", fmt.Errorf("the pane's history changed; select again")
	}
	from, to := [2]int{r.FromY, r.FromX}, [2]int{r.ToY, r.ToX}
	if to[0] < from[0] || (to[0] == from[0] && to[1] < from[1]) {
		from, to = to, from
	}
	left, right := min(r.FromX, r.ToX), max(r.FromX, r.ToX)
	var text strings.Builder
	for id := from[0]; id <= to[0]; id++ {
		row, wrapped, ok := p.historyRowLocked(id)
		if !ok {
			return "", fmt.Errorf("the selection reaches rows that are no longer kept")
		}
		lo, hi := 0, len(row)-1
		switch r.Mode {
		case "block":
			lo, hi, wrapped = left, right, false
		case "char":
			if id == from[0] {
				lo = from[1]
			}
			if id == to[0] {
				hi = to[1]
			}
		case "line":
		default:
			return "", fmt.Errorf("unknown selection mode %q", r.Mode)
		}
		line := rowText(row, lo, hi)
		if id < to[0] && wrapped {
			text.WriteString(line)
		} else {
			text.WriteString(strings.TrimRight(line, " "))
			if id < to[0] {
				text.WriteByte('\n')
			}
		}
		if text.Len() > protocol.MaxPasteBytes {
			return "", fmt.Errorf("the selection is too large to copy")
		}
	}
	return text.String(), nil
}

// rowText is columns lo..hi of a row, wide characters whole: a range that
// starts inside one takes all of it.
func rowText(row uv.Line, lo, hi int) string {
	hi = min(hi, len(row)-1)
	for lo > 0 && lo < len(row) && row[lo].IsZero() {
		lo--
	}
	var b strings.Builder
	for x := max(0, lo); x <= hi; x++ {
		c := row[x]
		switch {
		case c.IsZero():
		case c.Style.Attrs&uv.AttrConceal != 0 || c.Content == "":
			b.WriteString(strings.Repeat(" ", max(1, c.Width)))
		default:
			b.WriteString(c.Content)
		}
	}
	return b.String()
}

// historyText is a pane's history text for the client's keyboard selection;
// pane 0 is the active window's focused pane.
func (s *session) historyText(id int, r *protocol.TextRange) (string, error) {
	if r == nil {
		return "", fmt.Errorf("history-text needs a range")
	}
	p, err := s.livePane(id)
	if err != nil {
		return "", err
	}
	return p.historyText(*r)
}
