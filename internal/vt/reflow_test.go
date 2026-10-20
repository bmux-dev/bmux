package vt

import (
	"strings"
	"testing"
)

// screenLines is the showing screen's rows, trailing blanks trimmed.
func screenLines(e *Emulator) []string {
	var lines []string
	for y := range e.Height() {
		var b strings.Builder
		for x := range e.Width() {
			if c := e.CellAt(x, y); c != nil && c.Width > 0 {
				b.WriteString(c.Content)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return lines
}

func historyLines(e *Emulator) []string {
	var lines []string
	for y := range e.ScrollbackLen() {
		var b strings.Builder
		for x := 0; ; x++ {
			c := e.ScrollbackCellAt(x, y)
			if c == nil {
				break
			}
			if c.Width > 0 {
				b.WriteString(c.Content)
			}
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return lines
}

func wrappedRows(e *Emulator) []bool {
	flags := make([]bool, e.Height())
	for y := range flags {
		flags[y] = e.LineWrapped(y)
	}
	return flags
}

func expectLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func expectCursor(t *testing.T, e *Emulator, x, y int) {
	t.Helper()
	if p := e.CursorPosition(); p.X != x || p.Y != y {
		t.Fatalf("cursor at %d,%d, want %d,%d", p.X, p.Y, x, y)
	}
}

func TestReflowJoinsAndSplitsWrappedLines(t *testing.T) {
	e := NewEmulator(5, 4)
	_, _ = e.WriteString("abcdefghij")
	expectLines(t, "written at 5 columns", screenLines(e), []string{"abcde", "fghij", "", ""})
	if got := wrappedRows(e); !got[0] || got[1] {
		t.Fatalf("wrap flags %v", got)
	}

	e.Resize(10, 4)
	expectLines(t, "widened", screenLines(e), []string{"abcdefghij", "", "", ""})
	expectCursor(t, e, 9, 0)
	// The cursor still awaits the wrap: the next character starts row 1.
	_, _ = e.WriteString("k")
	expectLines(t, "typed after widening", screenLines(e), []string{"abcdefghij", "k", "", ""})

	e.Resize(4, 4)
	expectLines(t, "narrowed", screenLines(e), []string{"abcd", "efgh", "ijk", ""})
	if got := wrappedRows(e); !got[0] || !got[1] || got[2] {
		t.Fatalf("wrap flags after narrowing %v", got)
	}
	expectCursor(t, e, 3, 2)
}

func TestReflowKeepsHardNewlinesAndWideCharacters(t *testing.T) {
	e := NewEmulator(3, 4)
	_, _ = e.WriteString("abc\r\ndef")
	e.Resize(10, 4)
	expectLines(t, "two printed lines stay two", screenLines(e), []string{"abc", "def", "", ""})

	e = NewEmulator(10, 3)
	_, _ = e.WriteString("ab中cd")
	e.Resize(3, 3)
	// 中 is two columns: it can't follow "ab" in three, so it moves down.
	expectLines(t, "wide character kept whole", screenLines(e), []string{"ab", "中c", "d"})
	if got := wrappedRows(e); !got[0] || !got[1] || got[2] {
		t.Fatalf("wrap flags %v", got)
	}
}

func TestReflowRewrapsHistoryAndMovesOverflowIntoIt(t *testing.T) {
	e := NewEmulator(5, 2)
	_, _ = e.WriteString("12345678\r\nxy\r\nprompt")
	// At 5x2 "12345", "678", "xy" scrolled off; "promp", "t" are on screen.
	expectLines(t, "history at 5 columns", historyLines(e), []string{"12345", "678", "xy"})
	if !e.ScrollbackWrapped(0) || e.ScrollbackWrapped(1) {
		t.Fatal("scrollback lost the wrap")
	}

	e.Resize(10, 2)
	expectLines(t, "history widened", historyLines(e), []string{"12345678", "xy"})
	expectLines(t, "screen widened", screenLines(e), []string{"prompt", ""})
	expectCursor(t, e, 6, 0)

	// Shrinking the height moves rows above the cursor into history
	// instead of cropping them.
	e = NewEmulator(10, 4)
	_, _ = e.WriteString("one\r\ntwo\r\nthree\r\nfour")
	e.Resize(10, 2)
	expectLines(t, "screen after shrinking", screenLines(e), []string{"three", "four"})
	expectLines(t, "history after shrinking", historyLines(e), []string{"one", "two"})
	expectCursor(t, e, 4, 1)
}

func TestReflowDoesNotRefillAClearedScreen(t *testing.T) {
	e := NewEmulator(5, 3)
	_, _ = e.WriteString("aaaaaaa\r\nbbb\r\n\x1b[2J\x1b[H$ ")
	history := historyLines(e)
	e.Resize(10, 3)
	expectLines(t, "screen", screenLines(e), []string{"$", "", ""})
	if got := historyLines(e); len(got) > len(history) {
		t.Fatalf("history grew from %q to %q", history, got)
	}
}

func TestErasingALinesEndEndsItsWrap(t *testing.T) {
	e := NewEmulator(5, 3)
	_, _ = e.WriteString("abcdefg")
	if !e.LineWrapped(0) {
		t.Fatal("not wrapped")
	}
	_, _ = e.WriteString("\x1b[1;3H\x1b[K") // row 1, column 3: erase to the end
	if e.LineWrapped(0) {
		t.Fatal("erasing through the last column kept the wrap")
	}
	e.Resize(10, 3)
	expectLines(t, "after erase and widen", screenLines(e), []string{"ab", "fg", ""})
}

func TestAlternateScreenIsCroppedWhileMainScreenReflows(t *testing.T) {
	e := NewEmulator(5, 3)
	_, _ = e.WriteString("abcdefgh")
	_, _ = e.WriteString("\x1b[?1049h") // full-screen program
	_, _ = e.WriteString("\x1b[HALTERNATE")
	e.Resize(10, 3)
	expectLines(t, "alternate screen", screenLines(e), []string{"ALTER", "NATE", ""})
	_, _ = e.WriteString("\x1b[?1049l")
	expectLines(t, "main screen after returning", screenLines(e), []string{"abcdefgh", "", ""})
}
