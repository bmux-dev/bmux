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

// promptRows lists where prompts start: scrollback lines as "h<i>", screen
// rows as "s<y>".
func promptRows(e *Emulator) []string {
	var rows []string
	for y := range e.ScrollbackLen() {
		if e.ScrollbackPrompt(y) {
			rows = append(rows, "h"+string(rune('0'+y)))
		}
	}
	for y := range e.Height() {
		if e.LinePrompt(y) {
			rows = append(rows, "s"+string(rune('0'+y)))
		}
	}
	return rows
}

func TestPromptMarksFollowTheirLines(t *testing.T) {
	e := NewEmulator(6, 3)
	prompt := func(text string) {
		e.MarkPrompt()
		_, _ = e.WriteString(text)
	}
	prompt("$ ls\r\n")
	_, _ = e.WriteString("a b\r\n")
	prompt("$ make all")
	// "$ make all" wraps at the bottom, scrolling "$ ls" into history; the
	// wrapped line's mark is on its first row.
	expectLines(t, "rows", promptRows(e), []string{"h0", "s1"})
	_, _ = e.WriteString("\r\nok\r\n")
	expectLines(t, "after scrolling into history", promptRows(e), []string{"h0", "h2"})

	e.Resize(12, 3)
	// Reflow joins "$ make" and " all": the mark stays on the line's first row.
	if got := promptRows(e); len(got) != 2 || got[0] != "h0" {
		t.Fatalf("after reflow %v", got)
	}
	expectLines(t, "history after reflow", historyLines(e), []string{"$ ls", "a b"})

	// Erasing a prompt's whole row drops its mark; full-screen apps keep none.
	e = NewEmulator(10, 3)
	prompt("$ ")
	_, _ = e.WriteString("\x1b[2K")
	if got := promptRows(e); len(got) != 0 {
		t.Fatalf("erased row kept its mark: %v", got)
	}
	_, _ = e.WriteString("\x1b[?1049h")
	e.MarkPrompt()
	_, _ = e.WriteString("\x1b[?1049l")
	if got := promptRows(e); len(got) != 0 {
		t.Fatalf("alternate screen marked a prompt: %v", got)
	}
}

func TestImageRowsAreClippedNotWrapped(t *testing.T) {
	const cell = "\U0010EEEE\u0305\u0305"
	term := newTestTerminal(t, 12, 4)
	term.SetScrollbackSize(10)
	term.WriteString("\x1b[38;2;0;0;7m" + strings.Repeat(cell, 10) + "\x1b[0m\r\nplain words here")
	term.Resize(6, 4)
	rows := screenLines(term)
	if got := strings.Count(rows[0], cell); got != 6 || rows[1] != "plain" {
		t.Fatalf("narrowed screen: %q (image cells in row 0: %d)", rows, got)
	}
	if wrapped := wrappedRows(term); wrapped[0] {
		t.Fatal("clipped image row is marked as wrapping onto the next")
	}
	if rows[2] != "words" || rows[3] != "here" {
		t.Fatalf("ordinary text must still rewrap: %q", rows)
	}
	// Scrolled into history, the row stays clipped through further reflows.
	term.WriteString("\r\n\r\n\r\n\r\n")
	term.Resize(4, 4)
	for _, line := range historyLines(term) {
		if strings.Contains(line, cell) && strings.Count(line, cell) > 4 {
			t.Fatalf("history row grew past the width: %q", line)
		}
	}
}
