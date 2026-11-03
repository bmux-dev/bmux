package vt

import (
	"fmt"
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Scrolling by rotating whole rows must leave exactly the cells uv's
// cell-by-cell copy leaves, for every position, count and region.
func TestMoveLinesMatchesCellCopy(t *testing.T) {
	const w, h = 6, 5
	fill := func(s *Screen) {
		for y := range h {
			for x := range w {
				s.buf.Lines[y][x] = uv.Cell{Content: string(rune('a' + y*w + x)), Width: 1}
			}
		}
		s.buf.Lines[2][1] = uv.Cell{Content: "界", Width: 2} // a wide character and its placeholder
		s.buf.Lines[2][2] = uv.Cell{}
	}
	for _, region := range [][2]int{{0, h}, {1, 4}} {
		for _, bg := range []color.Color{nil, color.RGBA{R: 200, A: 255}} {
			for y := region[0]; y < region[1]; y++ {
				for n := -h - 1; n <= h+1; n++ {
					name := fmt.Sprintf("region %v bg %v y %d n %d", region, bg != nil, y, n)
					fast, slow := NewScreen(w, h), NewScreen(w, h)
					fill(fast)
					fill(slow)
					area := uv.Rect(0, region[0], w, region[1]-region[0])
					fast.cur.Pen.Bg, slow.cur.Pen.Bg = bg, bg
					fast.moveLines(y, n, area)
					if n > 0 {
						slow.buf.InsertLineArea(y, n, slow.blankCell(), area)
					} else if n < 0 {
						slow.buf.DeleteLineArea(y, -n, slow.blankCell(), area)
					}
					for row := range h {
						for x := range w {
							if got, want := fast.buf.Lines[row][x], slow.buf.Lines[row][x]; !got.Equal(&want) {
								t.Fatalf("%s: cell %d,%d is %+v, want %+v", name, x, row, got, want)
							}
						}
					}
				}
			}
		}
	}
}

// The rows a scroll frees are blanked, not shared with the rows that moved:
// writing to one must not show up in another.
func TestMoveLinesKeepsRowsDistinct(t *testing.T) {
	s := NewScreen(4, 3)
	s.moveLines(0, -1, s.buf.Bounds())
	s.buf.Lines[2][0] = uv.Cell{Content: "z", Width: 1}
	for y := range 2 {
		if s.buf.Lines[y][0].Content == "z" {
			t.Fatalf("row %d shares cells with the freed row", y)
		}
	}
}

// Printing past the bottom of a full history keeps the newest lines in order.
func TestScrollbackDropsOldestInOrder(t *testing.T) {
	term := NewEmulator(5, 2)
	term.SetScrollbackSize(3)
	for i := range 10 {
		term.WriteString(fmt.Sprintf("%d\r\n", i))
	}
	var got []string
	for i := range term.ScrollbackLen() {
		got = append(got, term.Scrollback().Line(i).String())
	}
	if want := []string{"6", "7", "8"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("history %q, want %q", got, want)
	}
}

// Output that scrolls a 120x40 screen with a full 10,000-line history, the
// common case for builds and logs.
func BenchmarkScrollingOutput(b *testing.B) {
	term := NewEmulator(120, 40)
	term.SetScrollbackSize(10000)
	var chunk []byte
	for i := range 1000 {
		chunk = fmt.Appendf(chunk, "%d some build output that fills part of the line\r\n", i)
	}
	for range 10 { // fill the history first
		_, _ = term.Write(chunk)
	}
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = term.Write(chunk)
	}
}
