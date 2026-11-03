package vt

import (
	"slices"

	uv "github.com/charmbracelet/ultraviolet"
)

// DefaultScrollbackSize is the default maximum number of lines in the scrollback buffer.
const DefaultScrollbackSize = 10000

// Scrollback represents a scrollback buffer that stores lines scrolled off the screen.
type Scrollback struct {
	lines []uv.Line
	// bmux: flags[i] is what's known about lines[i] besides its cells; see
	// [LineFlags].
	flags    []LineFlags
	maxLines int
}

// NewScrollback creates a new scrollback buffer with the given maximum number of lines.
func NewScrollback(maxLines int) *Scrollback {
	if maxLines <= 0 {
		maxLines = DefaultScrollbackSize
	}
	return &Scrollback{
		lines:    make([]uv.Line, 0, min(maxLines, 1000)), // Pre-allocate reasonable capacity
		flags:    make([]LineFlags, 0, min(maxLines, 1000)),
		maxLines: maxLines,
	}
}

// Push adds a line without flags to the scrollback buffer.
// If the buffer is full, the oldest line is removed.
func (s *Scrollback) Push(line uv.Line) {
	s.PushLine(line, 0)
}

// PushLine adds a line with its flags. A wrapped line keeps every cell,
// trailing blanks included, because they are part of the text that
// continues; other lines lose trailing blanks. bmux: added for reflow.
func (s *Scrollback) PushLine(line uv.Line, flags LineFlags) {
	if s == nil || s.maxLines <= 0 {
		return
	}

	end := len(line)
	if flags&FlagWrapped == 0 {
		// Find last non-empty cell to trim trailing empty cells.
		// This helps with wrapping and window resizing.
		end = 0
		for i := len(line) - 1; i >= 0; i-- {
			c := &line[i]
			if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
				end = i + 1
				break
			}
		}
	}

	// Clone the line content up to and including the last kept cell
	cloned := slices.Clone(line[:end])

	if len(s.lines) >= s.maxLines {
		// Drop the oldest line by reslicing rather than shifting every line:
		// append copies the history only when it outgrows its array, so a
		// push costs O(1) amortized. Clearing the slot frees its cells.
		s.lines[0], s.lines = nil, s.lines[1:]
		s.flags = s.flags[1:]
	}
	s.lines = append(s.lines, cloned)
	s.flags = append(s.flags, flags)
}

// PushN adds n lines from the screen starting at line y to the scrollback,
// with their flags (indexed by screen row; may be nil).
func (s *Scrollback) PushN(buf *uv.RenderBuffer, flags []LineFlags, y, n int) {
	if s == nil || buf == nil || n <= 0 {
		return
	}

	for i := range min(n, buf.Height()-y) {
		if line := buf.Line(y + i); line != nil {
			var f LineFlags
			if y+i < len(flags) {
				f = flags[y+i]
			}
			s.PushLine(line, f)
		}
	}
}

// Len returns the number of lines in the scrollback buffer.
func (s *Scrollback) Len() int {
	if s == nil {
		return 0
	}
	return len(s.lines)
}

// MaxLines returns the maximum number of lines the scrollback buffer can hold.
func (s *Scrollback) MaxLines() int {
	if s == nil {
		return 0
	}
	return s.maxLines
}

// SetMaxLines sets the maximum number of lines in the scrollback buffer.
// If the current number of lines exceeds the new maximum, oldest lines are removed.
func (s *Scrollback) SetMaxLines(maxLines int) {
	if s == nil || maxLines <= 0 {
		return
	}

	s.maxLines = maxLines
	if excess := len(s.lines) - maxLines; excess > 0 {
		// Remove oldest lines, freeing their cells.
		clear(s.lines[:excess])
		s.lines = s.lines[excess:]
		s.flags = s.flags[excess:]
	}
}

// Line returns the line at the given index.
// Index 0 is the oldest line, Len()-1 is the most recent.
// Returns nil if index is out of bounds.
func (s *Scrollback) Line(index int) uv.Line {
	if s == nil || index < 0 || index >= len(s.lines) {
		return nil
	}
	return s.lines[index]
}

// LineFlags returns the flags of the line at index. bmux: added for reflow.
func (s *Scrollback) LineFlags(index int) LineFlags {
	if s == nil || index < 0 || index >= len(s.flags) {
		return 0
	}
	return s.flags[index]
}

// Wrapped reports whether the line at index continues onto the next one.
// bmux: added for reflow.
func (s *Scrollback) Wrapped(index int) bool {
	return s.LineFlags(index)&FlagWrapped != 0
}

// Lines returns all lines in the scrollback buffer.
// Index 0 is the oldest line.
func (s *Scrollback) Lines() []uv.Line {
	if s == nil {
		return nil
	}
	return s.lines
}

// Clear removes all lines from the scrollback buffer.
func (s *Scrollback) Clear() {
	if s == nil {
		return
	}
	s.lines = s.lines[:0]
	s.flags = s.flags[:0]
}

// replace swaps in a whole new history, newest last, trimmed to the limit.
// bmux: used by reflow, which rewraps history along with the screen.
func (s *Scrollback) replace(lines []uv.Line, flags []LineFlags) {
	if s == nil {
		return
	}
	if excess := len(lines) - s.maxLines; excess > 0 {
		lines, flags = lines[excess:], flags[excess:]
	}
	s.lines, s.flags = lines, flags
}

// CellAt returns the cell at the given position in the scrollback buffer.
// x is the column, y is the line index (0 = oldest).
// Returns nil if position is out of bounds.
func (s *Scrollback) CellAt(x, y int) *uv.Cell {
	line := s.Line(y)
	if line == nil || x < 0 || x >= len(line) {
		return nil
	}
	return &line[x]
}
