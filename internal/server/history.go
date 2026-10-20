package server

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
