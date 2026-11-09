package client

import "github.com/gdamore/tcell/v2"

// paintScreen stages one complete draw without clearing the physical screen.
// Only final cell changes reach tcell, preserving its dirty-cell tracking.
// Input, cursor, capabilities and terminal-specific operations still use Screen.
type paintScreen struct {
	tcell.Screen
	cells tcell.CellBuffer
	style tcell.Style
}

func (s *paintScreen) begin(screen tcell.Screen, width, height int, style tcell.Style) {
	s.Screen = screen
	s.cells.Resize(width, height)
	s.SetStyle(style)
	s.Clear()
}

func (s *paintScreen) SetStyle(style tcell.Style) {
	s.style = style
	s.Screen.SetStyle(style)
}

func (s *paintScreen) Clear() { s.Fill(' ', s.style) }

func (s *paintScreen) Fill(r rune, style tcell.Style) { s.cells.Fill(r, style) }

func (s *paintScreen) Put(x, y int, str string, style tcell.Style) (string, int) {
	return s.cells.Put(x, y, str, style)
}

func (s *paintScreen) PutStrStyled(x, y int, str string, style tcell.Style) {
	w, h := s.cells.Size()
	for str != "" && x < w && y < h {
		var width int
		str, width = s.Put(x, y, str, style)
		if width == 0 {
			break
		}
		x += width
	}
}

func (s *paintScreen) PutStr(x, y int, str string) {
	s.PutStrStyled(x, y, str, tcell.StyleDefault)
}

func (s *paintScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	s.cells.SetContent(x, y, primary, combining, style)
}

func (s *paintScreen) SetCell(x, y int, style tcell.Style, chars ...rune) {
	if len(chars) == 0 {
		chars = []rune{' '}
	}
	s.Put(x, y, string(chars), style)
}

func (s *paintScreen) Get(x, y int) (string, tcell.Style, int) {
	return s.cells.Get(x, y)
}

func (s *paintScreen) GetContent(x, y int) (rune, []rune, tcell.Style, int) {
	return s.cells.GetContent(x, y)
}

func (s *paintScreen) flush() {
	w, h := s.cells.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			str, style, width := s.cells.Get(x, y)
			old, oldStyle, oldWidth := s.Screen.Get(x, y)
			if str != old || style != oldStyle || width != oldWidth {
				s.Screen.Put(x, y, str, style)
			}
			x += max(1, width)
		}
	}
}
