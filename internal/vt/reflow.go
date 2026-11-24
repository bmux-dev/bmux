package vt

// bmux: reflow. The main screen rewraps on resize like tmux and Ghostty:
// rows the terminal wrapped are joined into the lines the program printed,
// then wrapped again at the new width, in the screen and in scrollback.
// Screen lines stay on the screen and history stays history, so a cleared
// screen isn't refilled from scrollback; screen rows that no longer fit move
// into scrollback, oldest first. The cursor stays on the same character,
// and a prompt mark stays on the first row of its line.

// logicalLine is a printed line: its compact cells, sparse cell extras,
// from rows joined where wrapped, and whether a prompt starts it.
type logicalLine struct {
	cells  []Cell
	extras rowExtra // keyed by index into cells
	prompt bool
}

// joinRows turns compact rows, their optional extras, and flags into logical lines.
func joinRows(rows [][]Cell, extras []rowExtra, flags []LineFlags) []logicalLine {
	var lines []logicalLine
	var line logicalLine
	started := false
	for i, row := range rows {
		var f LineFlags
		if i < len(flags) {
			f = flags[i]
		}
		var ex rowExtra
		if i < len(extras) {
			ex = extras[i]
		}
		if !started {
			line.prompt, started = f&FlagPrompt != 0, true
		}
		base := len(line.cells)
		if f&FlagWrapped != 0 {
			line.cells = append(line.cells, row...)
			if len(ex) > 0 {
				if line.extras == nil {
					line.extras = make(rowExtra, len(ex))
				}
				for x, ce := range ex {
					if x >= 0 && x < len(row) {
						line.extras[base+x] = ce
					}
				}
			}
			continue
		}
		trimmed := trimCompactRow(row)
		line.cells = append(line.cells, trimmed...)
		if len(ex) > 0 {
			for x, ce := range ex {
				if x >= 0 && x < len(trimmed) {
					if line.extras == nil {
						line.extras = make(rowExtra, len(ex))
					}
					line.extras[base+x] = ce
				}
			}
		}
		lines = append(lines, line)
		line, started = logicalLine{}, false
	}
	if started {
		lines = append(lines, line)
	}
	return lines
}

// position is a cell in rewrapped rows.
type position struct{ row, col int }

// rewrap splits a logical line into rows of width compact cells with their
// optional extras and flags, never splitting a wide character, and reports
// where each old column landed: at[i] is the new position of the cell that
// started at old column i (columns inside a wide character map to its first
// cell). end is just past the last cell. A line always takes at least one row.
func rewrap(line logicalLine, width int) (rows [][]Cell, extras []rowExtra, flags []LineFlags, at []position, end position) {
	if width <= 0 {
		return nil, nil, nil, nil, position{}
	}
	if hasPlaceholder(line) {
		return clip(line, width)
	}
	row := newCompactRow(width)
	var re rowExtra
	col := 0
	at = make([]position, len(line.cells))
	for i, c := range line.cells {
		if c.IsZero() {
			// A wide character's trailing cells; the first one placed them.
			if i > 0 {
				at[i] = at[i-1]
			}
			continue
		}
		w := max(1, c.Width())
		if col+w > width && col > 0 {
			rows = append(rows, row)
			extras = append(extras, re)
			row, re, col = newCompactRow(width), nil, 0
		}
		at[i] = position{len(rows), col}
		if w > width {
			// Wider than the screen: keep its place, show a blank.
			c, w = EmptyCell, 1
		}
		row[col] = c
		if c.HasExtra() && line.extras != nil {
			if ce, ok := line.extras[i]; ok {
				if re == nil {
					re = make(rowExtra)
				}
				re[col] = ce
			}
		}
		for j := 1; j < w; j++ {
			row[col+j] = Cell{}
		}
		col += w
	}
	end = position{len(rows), col}
	rows = append(rows, row)
	extras = append(extras, re)
	flags = make([]LineFlags, len(rows))
	for j := range flags[:len(flags)-1] {
		flags[j] = FlagWrapped
	}
	if line.prompt {
		flags[0] |= FlagPrompt
	}
	return rows, extras, flags, at, end
}

// hasPlaceholder reports a line with image placeholder cells.
func hasPlaceholder(line logicalLine) bool {
	for _, c := range line.cells {
		if c.IsPlaceholder() {
			return true
		}
	}
	return false
}

// clip fits a line with image cells to one row, dropping what doesn't fit.
// An image's cells name the part of the image they show, so wrapping the end
// of a row onto the next would draw those parts under the wrong rows.
func clip(line logicalLine, width int) (rows [][]Cell, extras []rowExtra, flags []LineFlags, at []position, end position) {
	if width <= 0 {
		return nil, nil, nil, nil, position{}
	}
	row := newCompactRow(width)
	var re rowExtra
	at = make([]position, len(line.cells))
	col := 0
	for i, c := range line.cells {
		if c.IsZero() {
			if i > 0 {
				at[i] = at[i-1]
			}
			continue
		}
		w := max(1, c.Width())
		if col+w > width {
			at[i] = position{0, width - 1}
			continue
		}
		at[i] = position{0, col}
		row[col] = c
		if c.HasExtra() && line.extras != nil {
			if ce, ok := line.extras[i]; ok {
				if re == nil {
					re = make(rowExtra)
				}
				re[col] = ce
			}
		}
		for j := 1; j < w; j++ {
			row[col+j] = Cell{}
		}
		col += w
	}
	flags = make([]LineFlags, 1)
	if line.prompt {
		flags[0] |= FlagPrompt
	}
	return [][]Cell{row}, []rowExtra{re}, flags, at, position{0, col}
}

// reflow resizes the screen, rewrapping wrapped lines when the width changes
// and moving rows that no longer fit into scrollback. phantom reports that
// the cursor is past the last column awaiting a wrap; the result is the same
// for the new layout.
func (s *Screen) reflow(width, height int, phantom bool) bool {
	oldWidth, oldHeight := s.width, s.height
	if width == oldWidth && height == oldHeight {
		return phantom
	}
	if width <= 0 || height <= 0 {
		s.Resize(max(width, 0), max(height, 0))
		s.cur.X = max(0, min(s.cur.X, max(width-1, 0)))
		s.cur.Y = max(0, min(s.cur.Y, max(height-1, 0)))
		s.saved.X = max(0, min(s.saved.X, max(width-1, 0)))
		s.saved.Y = max(0, min(s.saved.Y, max(height-1, 0)))
		return false
	}
	if oldWidth <= 0 || oldHeight <= 0 {
		s.Resize(width, height)
		s.cur.X = max(0, min(s.cur.X, width-1))
		s.cur.Y = max(0, min(s.cur.Y, height-1))
		s.saved.X = max(0, min(s.saved.X, width-1))
		s.saved.Y = max(0, min(s.saved.Y, height-1))
		return false
	}

	// The screen's rows up to the last one with content or the cursor;
	// blank rows below that are dropped and refilled.
	cx, cy := s.cur.X, s.cur.Y
	last := min(cy, oldHeight-1)
	for y := oldHeight - 1; y > last; y-- {
		if len(trimCompactRow(s.rows[y])) > 0 {
			last = y
			break
		}
	}
	screenRows := make([][]Cell, 0, last+1)
	screenExtras := make([]rowExtra, 0, last+1)
	for y := range last + 1 {
		screenRows = append(screenRows, s.rows[y])
		screenExtras = append(screenExtras, s.extras[y])
	}
	screenFlags := append([]LineFlags(nil), s.flags[:last+1]...)
	screenFlags[last] &^= FlagWrapped // nothing below to continue onto

	// Extract current scrollback rows (compact views into scrollback pages).
	var history [][]Cell
	var historyExtras []rowExtra
	var historyFlags []LineFlags
	if sb := s.scrollback; sb != nil && sb.Len() > 0 {
		n := sb.Len()
		history = make([][]Cell, n)
		historyExtras = make([]rowExtra, n)
		historyFlags = make([]LineFlags, n)
		for i := range n {
			history[i] = sb.RowCells(i)
			historyExtras[i] = sb.rowExtraAt(i)
			historyFlags[i] = sb.LineFlags(i)
		}
		// A line wrapped from scrollback onto the screen belongs to the screen.
		for len(history) > 0 && historyFlags[len(history)-1]&FlagWrapped != 0 {
			lastH := len(history) - 1
			screenRows = append([][]Cell{history[lastH]}, screenRows...)
			screenExtras = append([]rowExtra{historyExtras[lastH]}, screenExtras...)
			screenFlags = append([]LineFlags{historyFlags[lastH]}, screenFlags...)
			history = history[:lastH]
			historyExtras = historyExtras[:lastH]
			historyFlags = historyFlags[:lastH]
			cy++
		}
	}

	// The cursor's logical line and its column within it.
	cursorLine, cursorOffset := 0, 0
	for y, offset := 0, 0; y < len(screenRows); y++ {
		if y == cy {
			cursorOffset = offset + cx
			if phantom {
				cursorOffset++ // just past the character it sits on
			}
			break
		}
		if screenFlags[y]&FlagWrapped != 0 {
			offset += len(screenRows[y])
		} else {
			cursorLine, offset = cursorLine+1, 0
		}
	}

	var rows [][]Cell
	var rowsExtras []rowExtra
	var rowsFlags []LineFlags
	cursor := position{}
	for i, line := range joinRows(screenRows, screenExtras, screenFlags) {
		lineRows, lineExtras, lineFlags, at, end := rewrap(line, width)
		first := len(rows)
		if i == cursorLine {
			cursor, phantom = cursorPosition(line, at, end, cursorOffset, width, phantom)
			// A cursor past the line's text may need rows of its own.
			for len(lineRows) <= cursor.row {
				lineRows = append(lineRows, newCompactRow(width))
				lineExtras = append(lineExtras, nil)
				lineFlags = append(lineFlags, 0)
			}
			cursor.row += first
		}
		rows = append(rows, lineRows...)
		rowsExtras = append(rowsExtras, lineExtras...)
		rowsFlags = append(rowsFlags, lineFlags...)
	}

	// Rewrap history only when lines can change shape.
	if s.scrollback != nil && width != oldWidth && len(history) > 0 {
		var rewrapped [][]Cell
		var rewrappedExtras []rowExtra
		var flags []LineFlags
		for _, line := range joinRows(history, historyExtras, historyFlags) {
			lineRows, lineExtras, lineFlags, _, _ := rewrap(line, width)
			lastIdx := len(lineRows) - 1
			lineRows[lastIdx] = trimCompactRow(lineRows[lastIdx])
			lineExtras[lastIdx] = cloneRowExtra(lineExtras[lastIdx], len(lineRows[lastIdx]))
			rewrapped = append(rewrapped, lineRows...)
			rewrappedExtras = append(rewrappedExtras, lineExtras...)
			flags = append(flags, lineFlags...)
		}
		history, historyExtras, historyFlags = rewrapped, rewrappedExtras, flags
	}

	// Rows that no longer fit go to scrollback, keeping the cursor on screen.
	if excess := len(rows) - height; excess > 0 {
		for i := range excess {
			row := rows[i]
			ex := rowsExtras[i]
			if rowsFlags[i]&FlagWrapped == 0 {
				row = trimCompactRow(row)
				ex = cloneRowExtra(ex, len(row))
			}
			history = append(history, row)
			historyExtras = append(historyExtras, ex)
			historyFlags = append(historyFlags, rowsFlags[i])
		}
		rows = rows[excess:]
		rowsExtras = rowsExtras[excess:]
		rowsFlags = rowsFlags[excess:]
		cursor.row -= excess
	}
	if s.scrollback != nil {
		// replaceCompact copies trimmed cells into fresh packed pages,
		// so no oversized backing arrays are ever retained in scrollback.
		s.scrollback.replaceCompact(history, historyExtras, historyFlags)
	}

	s.width = width
	s.height = height
	s.rows = make([][]Cell, height)
	s.extras = nil
	s.flags = resizeFlags(s.flags[:0], height)
	s.rowRev = make([]uint64, height)
	for y := range height {
		row := newCompactRow(width)
		if y < len(rows) {
			copy(row, rows[y])
			s.flags[y] = rowsFlags[y]
			if len(rowsExtras[y]) > 0 {
				if s.extras == nil {
					s.extras = make(map[int]rowExtra)
				}
				s.extras[y] = rowsExtras[y]
			}
		}
		s.rows[y] = row
	}
	s.bumpAllRows()
	s.scroll = s.Bounds()

	s.cur.X = max(0, min(cursor.col, width-1))
	s.cur.Y = max(0, min(cursor.row, height-1))
	s.saved.X = max(0, min(s.saved.X, width-1))
	s.saved.Y = max(0, min(s.saved.Y, height-1))
	return phantom
}

// cursorPosition places a cursor that was offset columns into a logical
// line, and reports whether it now awaits a wrap. Past the line's text it
// continues as if the line went on in blanks. A phantom cursor (just past
// the character at offset-1, awaiting a wrap) goes after that character,
// staying phantom only if that is the end of a row.
func cursorPosition(line logicalLine, at []position, end position, offset, width int, phantom bool) (position, bool) {
	if phantom && offset > 0 && offset-1 < len(at) {
		i := offset - 1
		// If offset-1 lands on a wide character's trailing zero cell, step back
		// to its leading cell so we measure its real display width.
		for i > 0 && line.cells[i].IsZero() {
			i--
		}
		p := at[i]
		w := max(1, line.cells[i].Width())
		if p.col+w < width {
			return position{p.row, p.col + w}, false
		}
		return position{p.row, width - 1}, true
	}
	if !phantom && offset < len(at) {
		return at[offset], false
	}
	// Past the text: count on from just after the last cell.
	column := end.row*width + end.col + (offset - len(at))
	return position{column / width, column % width}, false
}
