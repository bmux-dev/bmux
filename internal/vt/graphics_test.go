package vt

import (
	"strings"
	"testing"
	"time"
)

// replies collects what the emulator writes back to the app.
func replies(term *Emulator) func() string {
	ch := make(chan string, 64)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := term.Read(buf)
			if n > 0 {
				ch <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return func() string {
		select {
		case s := <-ch:
			return s
		case <-time.After(100 * time.Millisecond):
			return ""
		}
	}
}

type fakeGraphics struct {
	got   []string
	reply string
}

func (f *fakeGraphics) Kitty(payload []byte) string {
	f.got = append(f.got, string(payload))
	return f.reply
}

func TestKittyGraphicsSequencesReachTheHandler(t *testing.T) {
	term := newTestTerminal(t, 10, 3)
	read := replies(term)
	g := &fakeGraphics{reply: "\x1b_Gi=1;OK\x1b\\"}
	term.SetGraphics(g)
	term.WriteString("\x1b_Ga=t,f=100,i=1;QUJD\x1b\\" + "x")
	term.WriteString("\x1b_Ga=p,i=1,U=1\x1b\\")
	if len(g.got) != 2 || g.got[0] != "a=t,f=100,i=1;QUJD" || g.got[1] != "a=p,i=1,U=1" {
		t.Fatalf("handler saw %q", g.got)
	}
	if got := read(); got != g.reply {
		t.Fatalf("app got %q, want the handler's reply", got)
	}
	if text := termText(term)[0]; !strings.HasPrefix(text, "x") {
		t.Fatalf("graphics sequence leaked into the screen: %q", text)
	}
	// Other APC sequences still fall through unhandled.
	g.got = nil
	term.WriteString("\x1b_Hnot graphics\x1b\\")
	if len(g.got) != 0 {
		t.Fatalf("non-G APC reached the graphics handler: %q", g.got)
	}
}

func TestWindowSizeReports(t *testing.T) {
	term := newTestTerminal(t, 10, 4)
	read := replies(term)
	for _, q := range []string{"\x1b[14t", "\x1b[16t"} {
		term.WriteString(q)
		if got := read(); got != "" {
			t.Fatalf("%q answered %q before the cell size was known", q, got)
		}
	}
	term.WriteString("\x1b[18t")
	if got, want := read(), "\x1b[8;4;10t"; got != want {
		t.Fatalf("CSI 18t = %q, want %q", got, want)
	}
	term.SetCellPixels(9, 20)
	tests := map[string]string{
		"\x1b[14t": "\x1b[4;80;90t",
		"\x1b[16t": "\x1b[6;20;9t",
		"\x1b[18t": "\x1b[8;4;10t",
	}
	for q, want := range tests {
		term.WriteString(q)
		if got := read(); got != want {
			t.Errorf("%q = %q, want %q", q, got, want)
		}
	}
	term.WriteString("\x1b[?2048h")
	if got, want := read(), "\x1b[48;4;10;80;90t"; got != want {
		t.Errorf("in-band resize = %q, want %q", got, want)
	}
}

func TestPlaceholderImageIDs(t *testing.T) {
	term := newTestTerminal(t, 8, 2)
	term.SetScrollbackSize(10)
	const (
		cell      = "\U0010EEEE\u0305\u0305"
		trueColor = "\x1b[38;2;0;1;2m" // id 0x000102 = 258
		palette   = "\x1b[38;5;7m"     // id 7
		reset     = "\x1b[0m"
	)
	term.WriteString(trueColor + cell + cell + reset + " plain\r\n")
	term.WriteString("\x1b[38;2;0;0;99m\U0010EEEE" + reset) // on row 1, scrolls with the next line
	term.WriteString("\r\n\r\n" + palette + cell + reset)
	// The 99 row scrolled into history; 258 row too.
	got := term.PlaceholderImageIDs()
	for _, id := range []uint32{258, 99, 7} {
		if _, ok := got[id]; !ok {
			t.Errorf("id %d not found in %v", id, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("found %v, want exactly 258, 99 and 7", got)
	}

	term.WriteString("\x1b[?1049h\x1b[38;2;0;0;5m" + cell)
	if _, ok := term.PlaceholderImageIDs()[5]; !ok {
		t.Error("alt screen placeholder not found")
	}
	// Text that merely contains the placeholder rune later in a cluster, or
	// ordinary cells, never counts.
	plain := newTestTerminal(t, 8, 1)
	plain.WriteString("\x1b[38;2;0;0;1mabc")
	if ids := plain.PlaceholderImageIDs(); len(ids) != 0 {
		t.Errorf("ordinary text produced ids %v", ids)
	}
}
