package server

import (
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestSearchHistoryFindsMatchesBothWaysAndWraps(t *testing.T) {
	p := shellless(10, 3)
	// Row 0-1: a wrapped URL. Rows 2-5: lines with "err" and "ERR".
	p.write("see https://x.io/err\r\nok\r\nerr one\r\nfine\r\nERR two")
	// Row IDs: 0 "see https:", 1 "//x.io/err", 2 "ok", 3 "err one",
	// 4 "fine", 5 "ERR two"; rows 3-5 are on screen.
	search := func(fromX, fromY int, query string, up bool) protocol.TextRange {
		t.Helper()
		got, err := p.searchHistory(protocol.TextRange{Epoch: 1, FromX: fromX, FromY: fromY}, query, up)
		if err != nil {
			t.Fatalf("search %q from %d,%d: %v", query, fromX, fromY, err)
		}
		return got
	}
	at := func(r protocol.TextRange) [2]int { return [2]int{r.FromY, r.FromX} }

	// Up from the bottom finds the newest first; lowercase ignores case.
	if got := at(search(0, 5, "err", true)); got != [2]int{3, 0} {
		t.Fatalf("first up: %v", got)
	}
	if got := at(search(0, 3, "err", true)); got != [2]int{1, 7} {
		t.Fatalf("second up: %v", got)
	}
	// Past the oldest match it wraps to the newest.
	if got := at(search(7, 1, "err", true)); got != [2]int{5, 0} {
		t.Fatalf("wrap up: %v", got)
	}
	if got := at(search(0, 3, "err", false)); got != [2]int{5, 0} {
		t.Fatalf("down: %v", got)
	}
	// An uppercase letter makes the search exact.
	if got := at(search(0, 5, "ERR", true)); got != [2]int{5, 0} {
		t.Fatalf("exact case wraps to the only match: %v", got)
	}
	// A match across a wrap reports its first and last cells.
	if got := search(0, 5, "https://x.io", true); got.FromY != 0 || got.FromX != 4 || got.ToY != 1 || got.ToX != 5 {
		t.Fatalf("wrapped match %+v", got)
	}
	if _, err := p.searchHistory(protocol.TextRange{Epoch: 1, FromY: 5}, "nope", true); err == nil {
		t.Fatal("a missing query matched")
	}
	if _, err := p.searchHistory(protocol.TextRange{Epoch: 9}, "err", true); err == nil {
		t.Fatal("a stale epoch was searched")
	}
}

func TestJumpPromptUsesShellIntegrationMarks(t *testing.T) {
	p := shellless(10, 2)
	p.trackPrompts()
	// OSC 133;A starts each prompt, as shell integration sends it.
	p.write("\x1b]133;A\x07$ one\r\nout\r\n\x1b]133;A;cl=m\x1b\\$ two\r\nout2\r\n")
	// Rows: 0 "$ one", 1 "out", 2 "$ two", 3 "out2", 4 "" (rows 3-4 on screen).
	jump := func(from int, up bool) (int, error) {
		r, err := p.jumpPrompt(protocol.TextRange{Epoch: 1, FromY: from}, up)
		return r.FromY, err
	}
	for _, tc := range []struct {
		from int
		up   bool
		want int
	}{{4, true, 2}, {2, true, 0}, {0, false, 2}, {1, false, 2}} {
		if got, err := jump(tc.from, tc.up); err != nil || got != tc.want {
			t.Fatalf("from %d up %v: %d %v, want %d", tc.from, tc.up, got, err, tc.want)
		}
	}
	if _, err := jump(0, true); err == nil || err.Error() != "no earlier prompt" {
		t.Fatalf("past the first prompt: %v", err)
	}
	if _, err := jump(2, false); err == nil || err.Error() != "no later prompt" {
		t.Fatalf("past the last prompt: %v", err)
	}
	plain := shellless(10, 2)
	plain.trackPrompts()
	plain.write("$ no\r\nintegration\r\n")
	if _, err := plain.jumpPrompt(protocol.TextRange{Epoch: 1, FromY: 2}, true); err == nil || !strings.Contains(err.Error(), "OSC 133") {
		t.Fatalf("without marks: %v", err)
	}
}
