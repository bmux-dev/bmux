package vt

import (
	"fmt"
	"slices"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

// Sequences programs use that bmux once got wrong; esctest found each one.
func TestCoreSequences(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []string
		pos   uv.Position
	}{
		{
			name:  "NEL starts the next line",
			input: "ab\x1bEc",
			want:  []string{"ab   ", "c    ", "     "},
			pos:   uv.Pos(1, 1),
		},
		{
			name:  "NEL scrolls at the bottom",
			input: "a\r\nb\r\nc\x1bEd",
			want:  []string{"b    ", "c    ", "d    "},
			pos:   uv.Pos(1, 2),
		},
		{
			name:  "ED 1 stops at the cursor",
			input: "abcde\r\nfghij\r\nklmno\x1b[2;3H\x1b[1J",
			want:  []string{"     ", "   ij", "klmno"},
			pos:   uv.Pos(2, 1),
		},
		{
			name:  "ED 3 leaves the screen",
			input: "abc\r\ndef\x1b[3J",
			want:  []string{"abc  ", "def  ", "     "},
			pos:   uv.Pos(3, 1),
		},
		{
			name:  "CSI u restores the cursor CSI s saved",
			input: "\x1b[2;4H\x1b[s\x1b[3;1Hx\x1b[uy",
			want:  []string{"     ", "   y ", "x    "},
			pos:   uv.Pos(4, 1),
		},
		{
			name:  "insert mode pushes the line right",
			input: "abcd\r\x1b[4hXY\x1b[4lZ",
			want:  []string{"XYZbc", "     ", "     "},
			pos:   uv.Pos(3, 0),
		},
		{
			name:  "insert mode stops at the right margin",
			input: "abcde\x1b[?69h\x1b[1;3s\x1b[1;1H\x1b[4hX",
			want:  []string{"Xabde", "     ", "     "},
			pos:   uv.Pos(1, 0),
		},
		{
			name:  "full reset turns insert mode off",
			input: "\x1b[4h\x1bcabc\rX",
			want:  []string{"Xbc  ", "     ", "     "},
			pos:   uv.Pos(1, 0),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := newTestTerminal(t, 5, 3)
			term.WriteString(tc.input)
			if got := termText(term); !slices.Equal(got, tc.want) {
				t.Errorf("screen %q, want %q", got, tc.want)
			}
			if got := term.CursorPosition(); got != tc.pos {
				t.Errorf("cursor at %v, want %v", got, tc.pos)
			}
		})
	}
}

// In origin mode a cursor position report is relative to the margins, so a
// program reads back the coordinates it moved to.
func TestCursorReportInOriginMode(t *testing.T) {
	for _, tc := range []struct {
		name, input, query, want string
	}{
		{"CPR", "\x1b[3;5r\x1b[?6h\x1b[2;4H", "\x1b[6n", "\x1b[2;4R"},
		{"DECXCPR", "\x1b[3;5r\x1b[?6h\x1b[2;4H", "\x1b[?6n", "\x1b[?2;4R"},
		{"left and right margins", "\x1b[3;5r\x1b[?69h\x1b[3;8s\x1b[?6h\x1b[2;4H", "\x1b[6n", "\x1b[2;4R"},
		{"without origin mode", "\x1b[3;5r\x1b[2;4H", "\x1b[6n", "\x1b[2;4R"},
		{"after leaving origin mode", "\x1b[3;5r\x1b[?6h\x1b[2;4H\x1b[?6l", "\x1b[6n", "\x1b[1;1R"},
		{"entering origin mode homes to the margins", "\x1b[3;5r\x1b[5;7H\x1b[?6h", "\x1b[6n", "\x1b[1;1R"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := newTestTerminal(t, 10, 6)
			read := replies(term)
			term.WriteString(tc.input + tc.query)
			if got := read(); got != tc.want {
				t.Fatalf("reply %q, want %q", got, tc.want)
			}
		})
	}
}

// DECSET 47 switches screens without erasing or moving the cursor, and each
// screen keeps its own saved cursor.
func TestLegacyAltScreen(t *testing.T) {
	term := newTestTerminal(t, 5, 3)
	term.WriteString("main\x1b[2;2H\x1b7\x1b[?47h")
	if !term.IsAltScreen() {
		t.Fatal("DECSET 47 didn't switch to the alternate screen")
	}
	if got := term.CursorPosition(); got != uv.Pos(1, 1) {
		t.Errorf("entering moved the cursor to %v", got)
	}
	term.WriteString("\x1b[3;4Hal\x1b[3;4H\x1b7\x1b[?47l")
	if term.IsAltScreen() {
		t.Fatal("DECRESET 47 didn't switch back")
	}
	if got := term.CursorPosition(); got != uv.Pos(3, 2) {
		t.Errorf("leaving moved the cursor to %v, want it kept at 3,2", got)
	}
	if got := fmt.Sprint(termText(term)); got != fmt.Sprint([]string{"main ", "     ", "     "}) {
		t.Errorf("main screen %s after returning", got)
	}
	term.WriteString("\x1b8")
	if got := term.CursorPosition(); got != uv.Pos(1, 1) {
		t.Errorf("main screen restored the cursor to %v, want its own 1,1", got)
	}
	term.WriteString("\x1b[?47h")
	if got := termText(term)[2]; got != "   al" {
		t.Errorf("alternate screen row 3 is %q after switching back, want it kept", got)
	}
	term.WriteString("\x1b8")
	if got := term.CursorPosition(); got != uv.Pos(3, 2) {
		t.Errorf("alternate screen restored the cursor to %v, want its own 3,2", got)
	}
}

// Like 47, DECSET 1047 keeps the cursor where it is both ways; 1049 enters
// at home and returns to the main screen's cursor.
func TestAltScreenCursor(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		enter, exit uv.Position
	}{
		{"1047", uv.Pos(1, 1), uv.Pos(3, 2)},
		{"1049", uv.Pos(0, 0), uv.Pos(1, 1)},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			term := newTestTerminal(t, 5, 3)
			term.WriteString("\x1b[2;2H\x1b[?" + tc.mode + "h")
			if got := term.CursorPosition(); got != tc.enter {
				t.Errorf("entering put the cursor at %v, want %v", got, tc.enter)
			}
			term.WriteString("\x1b[3;4H\x1b[?" + tc.mode + "l")
			if got := term.CursorPosition(); got != tc.exit {
				t.Errorf("leaving put the cursor at %v, want %v", got, tc.exit)
			}
		})
	}
}

// In origin mode every absolute and relative move stays relative to the
// margins: rows 3-5 and columns 3-8 here, so the region's top-left is 2,2.
func TestCursorMovesInOriginMode(t *testing.T) {
	const region = "\x1b[3;5r\x1b[?69h\x1b[3;8s\x1b[?6h"
	for _, tc := range []struct {
		name, input string
		want        uv.Position
	}{
		{"CUP", "\x1b[2;4H", uv.Pos(5, 3)},
		{"HVP", "\x1b[2;4f", uv.Pos(5, 3)},
		{"CHA", "\x1b[2;4H\x1b[1G", uv.Pos(2, 3)},
		{"HPA keeps the row", "\x1b[2;1H\x1b[3`", uv.Pos(4, 3)},
		{"VPA keeps the column", "\x1b[1;2H\x1b[3d", uv.Pos(3, 4)},
		{"HPR", "\x1b[1;1H\x1b[2a", uv.Pos(4, 2)},
		{"VPR", "\x1b[1;1H\x1b[1e", uv.Pos(2, 3)},
		{"HPR stops at the right margin", "\x1b[1;1H\x1b[20a", uv.Pos(7, 2)},
		{"resetting DECLRMM drops the side margins", "\x1b[?69l\x1b[1;1H", uv.Pos(0, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := newTestTerminal(t, 10, 6)
			term.WriteString(region + tc.input)
			if got := term.CursorPosition(); got != tc.want {
				t.Errorf("cursor at %v, want %v", got, tc.want)
			}
		})
	}
}
