package server

const retainedHistoryLines = 10000

// Give x/vt a bounded scratch margin during each output operation, then trim
// back to the retained limit. We can observe newly added lines even when the
// retained ring is full, without parsing ANSI/Unicode ourselves or modifying cells.
// If an operation replaces more than the entire ring, old anchors are genuinely
// gone; explicitly start a new coordinate epoch instead of reusing their IDs.
// Caller holds pane.mu.
func (p *pane) trackHistory(operation func()) {
	before := p.term.ScrollbackLen()
	p.term.SetScrollbackSize(retainedHistoryLines*2 + 1)
	operation()
	after := p.term.ScrollbackLen()
	if after < before || after >= retainedHistoryLines*2+1 {
		p.epoch++
		p.historyTotal = min(after, retainedHistoryLines)
	} else {
		p.historyTotal += after - before
	}
	p.term.SetScrollbackSize(retainedHistoryLines)
}
