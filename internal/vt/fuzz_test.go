package vt

import (
	"io"
	"strings"
	"testing"
)

// FuzzEmulatorOutput feeds the emulator what a program might print: any
// bytes, split around a resize. Every byte a program writes reaches this
// parser, so it must never panic, and the cursor must stay on screen.
func FuzzEmulatorOutput(f *testing.F) {
	for _, c := range cases {
		f.Add([]byte(strings.Join(c.input, "")), uint8(c.w), uint8(c.h))
	}
	for _, seed := range []string{
		"\x1b[?1049h\x1b[2J\x1b[999;999H\x1b[6n\x1b[?1049l",                  // alt screen, far cursor, a query
		"line one\r\nline two that is long enough to wrap around\r\n\x1b[3S", // wrapping and scrolling
		"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\",                // a hyperlink
		"\x1b_Ga=p,U=1,i=1\x1b\\\x1b[38;2;0;0;1m\U0010EEEE\u0305\u0305",      // an image placeholder
		"\x1b[>1u\x1b[=5;2u\x1b[<9u\x1b[?u",                                  // kitty keyboard flags
		"\x1b]133;A\x07$ \x1b]133;B\x07",                                     // prompt marks
		"\x1b[10;5r\x1b[5;1H\x1b[L\x1b[M\x1b[2;3H\x1b[@\x1b[P",               // scroll region edits
		"é界🙂\u0301\u200d\t\b\x7f",                                            // wide, combining and control
	} {
		f.Add([]byte(seed), uint8(20), uint8(5))
	}
	f.Fuzz(func(t *testing.T, data []byte, w, h uint8) {
		term := NewEmulator(80, 24)
		term.SetScrollbackSize(50)
		// Replies (cursor reports, device attributes) go through a pipe that
		// blocks until read, as the server's input bridge reads it.
		go func() { _, _ = io.Copy(io.Discard, term) }()
		defer term.InputPipe().(io.Closer).Close()

		half := len(data) / 2
		_, _ = term.Write(data[:half])
		// The server never makes a pane narrower than 2 columns or shorter than 1 row.
		term.Resize(int(w)%160+2, int(h)%60+1)
		_, _ = term.Write(data[half:])

		width, height := term.Width(), term.Height()
		if c := term.CursorPosition(); c.X < 0 || c.Y < 0 || c.X > width || c.Y >= height {
			t.Fatalf("cursor %v outside a %dx%d screen", c, width, height)
		}
		for y := range height {
			for x := range width {
				_ = term.CellAt(x, y)
			}
		}
		for y := range term.ScrollbackLen() {
			for x := range width {
				_ = term.ScrollbackCellAt(x, y)
			}
		}
		_ = term.PlaceholderImageIDs()
	})
}
