package protocol

// ApplyTerminalDelta reconstructs a complete TerminalFrame from prev and an
// incoming frame that may be a full frame (Delta == false), an unchanged marker
// (Delta && Unchanged), or an incremental scroll + dirty-row delta.
//
// ApplyTerminalDelta never mutates prev or its Rows/Wrapped slices, so retained
// references (such as selections or previous frame snapshots) stay intact.
func ApplyTerminalDelta(prev, delta *TerminalFrame) *TerminalFrame {
	if delta == nil || !delta.Delta {
		return delta
	}
	if delta.Unchanged {
		if prev != nil {
			return prev
		}
		return delta
	}

	out := *delta
	out.Delta = false
	out.Unchanged = false
	out.ScrollUp = 0
	out.RowDeltas = nil

	h := 0
	if prev != nil {
		h = min(len(prev.Rows), MaxTerminalHeight)
	}
	for _, rd := range delta.RowDeltas {
		if rd.Y >= MaxTerminalHeight {
			continue
		}
		if rd.Y >= h {
			h = rd.Y + 1
		}
	}

	var rows [][]Cell
	if h > 0 {
		rows = make([][]Cell, h)
		if prev != nil && len(prev.Rows) > 0 {
			if delta.ScrollUp > 0 {
				if delta.ScrollUp < len(prev.Rows) {
					copy(rows, prev.Rows[delta.ScrollUp:])
				}
			} else {
				copy(rows, prev.Rows)
			}
		}
	}

	var wrapped []bool
	if prev != nil && len(prev.Wrapped) > 0 && h > 0 {
		src := prev.Wrapped
		if delta.ScrollUp > 0 {
			if delta.ScrollUp < len(src) {
				src = src[delta.ScrollUp:]
			} else {
				src = nil
			}
		}
		for i := 0; i < len(src) && i < h; i++ {
			if src[i] {
				if wrapped == nil {
					wrapped = make([]bool, h)
				}
				wrapped[i] = true
			}
		}
	}

	for _, rd := range delta.RowDeltas {
		if rd.Y < 0 || rd.Y >= len(rows) {
			continue
		}
		rows[rd.Y] = rd.Cells
		if rd.Wrapped {
			if wrapped == nil {
				wrapped = make([]bool, h)
			}
			wrapped[rd.Y] = true
		} else if wrapped != nil {
			wrapped[rd.Y] = false
		}
	}

	if wrapped != nil {
		anyWrapped := false
		for _, w := range wrapped {
			if w {
				anyWrapped = true
				break
			}
		}
		if !anyWrapped {
			wrapped = nil
		}
	}

	out.Rows = rows
	out.Wrapped = wrapped
	return &out
}
