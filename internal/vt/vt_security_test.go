package vt

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestHugeTabAndRepeatCountsBounded(t *testing.T) {
	term := newTestTerminal(t, 10, 4)
	start := time.Now()
	term.WriteString("A\x1b[999999999b\x1b[999999999I\x1b[999999999Z")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("huge tab/repeat counts took %v, expected bounded fast path", elapsed)
	}
	x, y := term.scr.CursorPosition()
	if x < 0 || x >= 10 || y < 0 || y >= 4 {
		t.Fatalf("cursor out of bounds after tab/repeat: (%d,%d)", x, y)
	}
}

func TestOscFieldsBounded(t *testing.T) {
	term := newTestTerminal(t, 20, 4)
	long := strings.Repeat("x", maxOscFieldBytes*3)
	term.WriteString("\x1b]2;" + long + "\x1b\\")
	term.WriteString("\x1b]7;" + long + "\x1b\\")
	term.WriteString("\x1b]8;;https://example.com/" + long + "\x1b\\")

	if len(term.title) > maxOscFieldBytes {
		t.Fatalf("title len = %d, want <= %d", len(term.title), maxOscFieldBytes)
	}
	if len(term.cwd) > maxOscFieldBytes {
		t.Fatalf("cwd len = %d, want <= %d", len(term.cwd), maxOscFieldBytes)
	}
	if term.scr.cur.Link.URL != "" {
		t.Fatalf("expected oversized OSC 8 hyperlink to be ignored, got len %d", len(term.scr.cur.Link.URL))
	}
}

func TestPasteStripsNestedBracketedPasteEnd(t *testing.T) {
	term := newTestTerminal(t, 20, 4)
	term.WriteString("\x1b[?2004h")

	outCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, term.pr)
		outCh <- buf.String()
	}()

	term.Paste("hi\x1b[201~there\x1b[2\x1b[201~01~end\x9b201~!")
	_ = term.Close()
	got := <-outCh
	want := "\x1b[200~hithereend!\x1b[201~"
	if got != want {
		t.Fatalf("Paste wrote %q, want %q", got, want)
	}
}
