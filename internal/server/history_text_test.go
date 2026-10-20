package server

import (
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/vt"
)

// shellless is a pane with an emulator and no shell, for history tests.
func shellless(width, height int) *pane {
	return &pane{term: vt.NewEmulator(width, height), epoch: 1}
}

func (p *pane) write(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trackHistory(func() { _, _ = p.term.WriteString(text) })
}

func TestHistoryTextSpansHistoryAndJoinsWraps(t *testing.T) {
	p := shellless(10, 3)
	// Rows 0-1 are one wrapped line; then three short lines; rows 0-2 end up
	// in history and rows 3-5 on screen.
	p.write("https://example.org/a\r\nalpha beta\r\ngamma\r\ndelta")
	text := func(r protocol.TextRange) string {
		t.Helper()
		r.Epoch = 1
		got, err := p.historyText(r)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := text(protocol.TextRange{FromX: 0, FromY: 0, ToX: 0, ToY: 2, Mode: "char"}); got != "https://example.org/a" {
		t.Fatalf("wrapped line across history: %q", got)
	}
	// Reversed ends and a partial last row; the screen rows come from the
	// live screen.
	if got := text(protocol.TextRange{FromX: 2, FromY: 5, ToX: 6, ToY: 3, Mode: "char"}); got != "beta\ngamma\ndel" {
		t.Fatalf("char selection: %q", got)
	}
	if got := text(protocol.TextRange{FromX: 4, FromY: 3, ToX: 1, ToY: 4, Mode: "line"}); got != "alpha beta\ngamma" {
		t.Fatalf("line selection: %q", got)
	}
	if got := text(protocol.TextRange{FromX: 1, FromY: 3, ToX: 3, ToY: 5, Mode: "block"}); got != "lph\namm\nelt" {
		t.Fatalf("block selection: %q", got)
	}

	if _, err := p.historyText(protocol.TextRange{Epoch: 2, Mode: "char"}); err == nil {
		t.Fatal("a stale epoch was accepted")
	}
	if _, err := p.historyText(protocol.TextRange{Epoch: 1, FromY: 0, ToY: 99, Mode: "char"}); err == nil || !strings.Contains(err.Error(), "no longer kept") {
		t.Fatalf("rows past the screen: %v", err)
	}
	p.write("\x1b[?1049h")
	if _, err := p.historyText(protocol.TextRange{Epoch: p.epoch, Mode: "char"}); err == nil {
		t.Fatal("history text from the alternate screen")
	}
}
