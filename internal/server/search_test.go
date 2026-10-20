package server

import (
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
