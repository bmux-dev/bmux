package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// bmux: reflow. The main screen rewraps on resize like tmux and Ghostty:
// rows the terminal wrapped are joined into the lines the program printed,
// then wrapped again at the new width, in the screen and in scrollback.
// Screen lines stay on the screen and history stays history, so a cleared
// screen isn't refilled from scrollback; screen rows that no longer fit move
// into scrollback, oldest first. The cursor stays on the same character,
// and a prompt mark stays on the first row of its line.

// logicalLine is a printed line: its cells, from rows joined where wrapped,
// and whether a prompt starts it.
type logicalLine struct {
	cells  []uv.Cell
	prompt bool
}

// trimRow drops trailing blank cells from a row that ends there.
func trimRow(row uv.Line) uv.Line {
	end := len(row)
	for end > 0 && (row[end-1].IsZero() || row[end-1].Equal(&uv.EmptyCell)) {
		end--
	}
	return row[:end]
}

// joinRows turns rows and their flags into logical lines.
func joinRows(rows []uv.Line, flags []LineFlags) []logicalLine {
	var lines []logicalLine
	var line logicalLine
	started := false
	for i, row := range rows {
		var f LineFlags
		if i < len(flags) {
			f = flags[i]
		}
		if !started {
			line.prompt, started = f&FlagPrompt != 0, true
		}
		if f&FlagWrapped != 0 {
			line.cells = append(line.cells, row...)
			continue
		}
		line.cells = append(line.cells, trimRow(row)...)
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

// rewrap splits a logical line into rows of width cells with their flags,
// never splitting a wide character, and reports where each old column
// landed: at[i] is the new position of the cell that started at old column
// i (columns inside a wide character map to its first cell). end is just
// past the last cell. A line always takes at least one row.
func rewrap(line logicalLine, width int) (rows []uv.Line, flags []LineFlags, at []position, end position) {
	if hasPlaceholder(line) {
		return clip(line, width)
	}
	row := newRow(width)
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
		w := max(1, c.Width)
		if col+w > width && col > 0 {
			rows = append(rows, row)
			row, col = newRow(width), 0
		}
		at[i] = position{len(rows), col}
		if w > width {
			// Wider than the screen: keep its place, show a blank.
			c, w = uv.EmptyCell, 1
		}
		row[col] = c
		for j := 1; j < w; j++ {
			row[col+j] = uv.Cell{}
		}
		col += w
	}
	end = position{len(rows), col}
	rows = append(rows, row)
	flags = make([]LineFlags, len(rows))
	for j := range flags[:len(flags)-1] {
		flags[j] = FlagWrapped
	}
	if line.prompt {
		flags[0] |= FlagPrompt
	}
	return rows, flags, at, end
}

// hasPlaceholder reports a line with image placeholder cells.
func hasPlaceholder(line logicalLine) bool {
	for _, c := range line.cells {
		if IsPlaceholder(c.Content) {
			return true
		}
	}
	return false
}

// clip fits a line with image cells to one row, dropping what doesn't fit.
// An image's cells name the part of the image they show, so wrapping the end
// of a row onto the next would draw those parts under the wrong rows.
func clip(line logicalLine, width int) (rows []uv.Line, flags []LineFlags, at []position, end position) {
	row := newRow(width)
	at = make([]position, len(line.cells))
	col := 0
	for i, c := range line.cells {
		if c.IsZero() {
			if i > 0 {
				at[i] = at[i-1]
			}
			continue
		}
		w := max(1, c.Width)
		if col+w > width {
			at[i] = position{0, width - 1}
			continue
		}
		at[i] = position{0, col}
		row[col] = c
		for j := 1; j < w; j++ {
			row[col+j] = uv.Cell{}
		}
		col += w
	}
	flags = make([]LineFlags, 1)
	if line.prompt {
		flags[0] |= FlagPrompt
	}
	return []uv.Line{row}, flags, at, position{0, col}
}

func newRow(width int) uv.Line {
	row := make(uv.Line, width)
	for i := range row {
		row[i] = uv.EmptyCell
	}
	return row
}

// reflow resizes the screen, rewrapping wrapped lines when the width changes
// and moving rows that no longer fit into scrollback. phantom reports that
// the cursor is past the last column awaiting a wrap; the result is the same
// for the new layout.
func (s *Screen) reflow(width, height int, phantom bool) bool {
	oldWidth, oldHeight := s.buf.Width(), s.buf.Height()
	if width == oldWidth && height == oldHeight {
		return phantom
	}

	// The screen's rows up to the last one with content or the cursor;
	// blank rows below that are dropped and refilled.
	cx, cy := s.cur.X, s.cur.Y
	last := cy
	for y := oldHeight - 1; y > cy; y-- {
		if len(trimRow(s.buf.Line(y))) > 0 {
			last = y
			break
		}
	}
	screenRows := make([]uv.Line, 0, last+1)
	for y := range last + 1 {
		screenRows = append(screenRows, s.buf.Line(y))
	}
	screenFlags := append([]LineFlags(nil), s.flags[:last+1]...)
	screenFlags[last] &^= FlagWrapped // nothing below to continue onto

	// A line wrapped from scrollback onto the screen belongs to the screen.
	var history []uv.Line
	var historyFlags []LineFlags
	if sb := s.scrollback; sb != nil {
		history, historyFlags = sb.lines, sb.flags
		for len(history) > 0 && historyFlags[len(history)-1]&FlagWrapped != 0 {
			n := len(history) - 1
			screenRows = append([]uv.Line{history[n]}, screenRows...)
			screenFlags = append([]LineFlags{historyFlags[n]}, screenFlags...)
			history, historyFlags = history[:n], historyFlags[:n]
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

	var rows []uv.Line
	var rowsFlags []LineFlags
	cursor := position{}
	for i, line := range joinRows(screenRows, screenFlags) {
		lineRows, lineFlags, at, end := rewrap(line, width)
		first := len(rows)
		if i == cursorLine {
			cursor, phantom = cursorPosition(line, at, end, cursorOffset, width, phantom)
			// A cursor past the line's text may need rows of its own.
			for len(lineRows) <= cursor.row {
				lineRows = append(lineRows, newRow(width))
				lineFlags = append(lineFlags, 0)
			}
			cursor.row += first
		}
		rows = append(rows, lineRows...)
		rowsFlags = append(rowsFlags, lineFlags...)
	}

	// Rewrap history only when lines can change shape.
	if s.scrollback != nil && width != oldWidth {
		var rewrapped []uv.Line
		var flags []LineFlags
		for _, line := range joinRows(history, historyFlags) {
			lineRows, lineFlags, _, _ := rewrap(line, width)
			lineRows[len(lineRows)-1] = trimRow(lineRows[len(lineRows)-1])
			rewrapped = append(rewrapped, lineRows...)
			flags = append(flags, lineFlags...)
		}
		history, historyFlags = rewrapped, flags
	}

	// Rows that no longer fit go to scrollback, keeping the cursor on screen.
	if excess := len(rows) - height; excess > 0 {
		history = append([]uv.Line(nil), history...)
		historyFlags = append([]LineFlags(nil), historyFlags...)
		for i := range excess {
			row := rows[i]
			if rowsFlags[i]&FlagWrapped == 0 {
				row = trimRow(row)
			}
			history = append(history, row)
			historyFlags = append(historyFlags, rowsFlags[i])
		}
		rows, rowsFlags = rows[excess:], rowsFlags[excess:]
		cursor.row -= excess
	}
	if s.scrollback != nil {
		s.scrollback.replace(history, historyFlags)
	}

	s.buf.Resize(width, height)
	s.buf.Touched = nil
	s.flags = resizeFlags(s.flags[:0], height)
	for y := range height {
		row := newRow(width)
		if y < len(rows) {
			copy(row, rows[y])
			s.flags[y] = rowsFlags[y]
		}
		copy(s.buf.Line(y), row)
	}
	s.scroll = s.buf.Bounds()

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
	if phantom && offset-1 < len(at) {
		i := offset - 1
		p := at[i]
		w := max(1, line.cells[i].Width)
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
