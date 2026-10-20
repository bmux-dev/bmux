package vt

import (
	uv "github.com/charmbracelet/ultraviolet"
)

// bmux: reflow. The main screen rewraps on resize like tmux and Ghostty:
// rows the terminal wrapped are joined into the lines the program printed,
// then wrapped again at the new width, in the screen and in scrollback.
// Screen lines stay on the screen and history stays history, so a cleared
// screen isn't refilled from scrollback; screen rows that no longer fit move
// into scrollback, oldest first. The cursor stays on the same character.

// logicalLine is a printed line: its cells, from rows joined where wrapped.
type logicalLine []uv.Cell

// trimRow drops trailing blank cells from a row that ends there.
func trimRow(row uv.Line) uv.Line {
	end := len(row)
	for end > 0 && (row[end-1].IsZero() || row[end-1].Equal(&uv.EmptyCell)) {
		end--
	}
	return row[:end]
}

// joinRows turns rows and their wrap flags into logical lines.
func joinRows(rows []uv.Line, wrapped []bool) []logicalLine {
	var lines []logicalLine
	var line logicalLine
	for i, row := range rows {
		if i < len(wrapped) && wrapped[i] {
			line = append(line, row...)
			continue
		}
		lines = append(lines, append(line, trimRow(row)...))
		line = nil
	}
	if line != nil {
		lines = append(lines, line)
	}
	return lines
}

// position is a cell in rewrapped rows.
type position struct{ row, col int }

// rewrap splits a logical line into rows of width cells, never splitting a
// wide character, and reports where each old column landed: at[i] is the
// new position of the cell that started at old column i (columns inside a
// wide character map to its first cell). end is just past the last cell. A
// line always takes at least one row.
func rewrap(line logicalLine, width int) (rows []uv.Line, at []position, end position) {
	row := newRow(width)
	col := 0
	at = make([]position, len(line))
	for i, c := range line {
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
	return append(rows, row), at, position{len(rows), col}
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
	screenWrapped := append([]bool(nil), s.wrapped[:last+1]...)
	screenWrapped[last] = false // nothing below to continue onto

	// A line wrapped from scrollback onto the screen belongs to the screen.
	var history []uv.Line
	var historyWrapped []bool
	if sb := s.scrollback; sb != nil {
		history, historyWrapped = sb.lines, sb.wrapped
		for len(history) > 0 && historyWrapped[len(history)-1] {
			n := len(history) - 1
			screenRows = append([]uv.Line{history[n]}, screenRows...)
			screenWrapped = append([]bool{true}, screenWrapped...)
			history, historyWrapped = history[:n], historyWrapped[:n]
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
		if screenWrapped[y] {
			offset += len(screenRows[y])
		} else {
			cursorLine, offset = cursorLine+1, 0
		}
	}

	var rows []uv.Line
	var rowsWrapped []bool
	cursor := position{}
	for i, line := range joinRows(screenRows, screenWrapped) {
		lineRows, at, end := rewrap(line, width)
		first := len(rows)
		if i == cursorLine {
			cursor, phantom = cursorPosition(line, at, end, cursorOffset, width, phantom)
			// A cursor past the line's text may need rows of its own.
			for len(lineRows) <= cursor.row {
				lineRows = append(lineRows, newRow(width))
			}
			cursor.row += first
		}
		for j, row := range lineRows {
			rows = append(rows, row)
			rowsWrapped = append(rowsWrapped, j < len(lineRows)-1)
		}
	}

	// Rewrap history only when lines can change shape.
	if s.scrollback != nil && width != oldWidth {
		var rewrapped []uv.Line
		var flags []bool
		for _, line := range joinRows(history, historyWrapped) {
			lineRows, _, _ := rewrap(line, width)
			for j, row := range lineRows {
				final := j == len(lineRows)-1
				if final {
					row = trimRow(row)
				}
				rewrapped = append(rewrapped, row)
				flags = append(flags, !final)
			}
		}
		history, historyWrapped = rewrapped, flags
	}

	// Rows that no longer fit go to scrollback, keeping the cursor on screen.
	if excess := len(rows) - height; excess > 0 {
		history = append([]uv.Line(nil), history...)
		historyWrapped = append([]bool(nil), historyWrapped...)
		for i := range excess {
			row := rows[i]
			if !rowsWrapped[i] {
				row = trimRow(row)
			}
			history = append(history, row)
			historyWrapped = append(historyWrapped, rowsWrapped[i])
		}
		rows, rowsWrapped = rows[excess:], rowsWrapped[excess:]
		cursor.row -= excess
	}
	if s.scrollback != nil {
		s.scrollback.replace(history, historyWrapped)
	}

	s.buf.Resize(width, height)
	s.buf.Touched = nil
	s.wrapped = resizeWrapped(s.wrapped[:0], height)
	for y := range height {
		row := newRow(width)
		if y < len(rows) {
			copy(row, rows[y])
			s.wrapped[y] = rowsWrapped[y]
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
		w := max(1, line[i].Width)
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
