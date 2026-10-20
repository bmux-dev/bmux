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
	// bmux: wrapped[i] reports that lines[i] continues onto the next line,
	// because the terminal wrapped it rather than the program ending it.
	wrapped  []bool
	maxLines int
}

// NewScrollback creates a new scrollback buffer with the given maximum number of lines.
func NewScrollback(maxLines int) *Scrollback {
	if maxLines <= 0 {
		maxLines = DefaultScrollbackSize
	}
	return &Scrollback{
		lines:    make([]uv.Line, 0, min(maxLines, 1000)), // Pre-allocate reasonable capacity
		wrapped:  make([]bool, 0, min(maxLines, 1000)),
		maxLines: maxLines,
	}
}

// Push adds a line that ends where it ends to the scrollback buffer.
// If the buffer is full, the oldest line is removed.
func (s *Scrollback) Push(line uv.Line) {
	s.PushWrapped(line, false)
}

// PushWrapped adds a line, recording whether it continues onto the next.
// A wrapped line keeps every cell, trailing blanks included, because they
// are part of the text that continues; other lines lose trailing blanks.
// bmux: added for reflow.
func (s *Scrollback) PushWrapped(line uv.Line, wrapped bool) {
	if s == nil || s.maxLines <= 0 {
		return
	}

	end := len(line)
	if !wrapped {
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
		// Remove oldest line and append new one
		s.lines = slices.Delete(s.lines, 0, 1)
		s.wrapped = slices.Delete(s.wrapped, 0, 1)
	}
	s.lines = append(s.lines, cloned)
	s.wrapped = append(s.wrapped, wrapped)
}

// PushN adds n lines from the screen starting at line y to the scrollback,
// with their wrap state from wrapped (indexed by screen row; may be nil).
func (s *Scrollback) PushN(buf *uv.RenderBuffer, wrapped []bool, y, n int) {
	if s == nil || buf == nil || n <= 0 {
		return
	}

	for i := range min(n, buf.Height()-y) {
		if line := buf.Line(y + i); line != nil {
			s.PushWrapped(line, y+i < len(wrapped) && wrapped[y+i])
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
	if len(s.lines) > maxLines {
		// Remove oldest lines
		s.lines = s.lines[len(s.lines)-maxLines:]
		s.wrapped = s.wrapped[len(s.wrapped)-maxLines:]
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

// Wrapped reports whether the line at index continues onto the next one.
// bmux: added for reflow.
func (s *Scrollback) Wrapped(index int) bool {
	return s != nil && index >= 0 && index < len(s.wrapped) && s.wrapped[index]
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
	s.wrapped = s.wrapped[:0]
}

// replace swaps in a whole new history, newest last, trimmed to the limit.
// bmux: used by reflow, which rewraps history along with the screen.
func (s *Scrollback) replace(lines []uv.Line, wrapped []bool) {
	if s == nil {
		return
	}
	if excess := len(lines) - s.maxLines; excess > 0 {
		lines, wrapped = lines[excess:], wrapped[excess:]
	}
	s.lines, s.wrapped = lines, wrapped
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
