package server

import (
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestRunShellShowsWhatItPrintedForTheFocusedPane(t *testing.T) {
	_, s := viewportSession(t)
	message := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			f, _ := s.snapshot(map[int]int{}, map[int]int{}, 0, true)
			if strings.Contains(f.Message, want) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("status message %q, want %q", f.Message, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	mustInput(t, s, protocol.Request{Op: "split", Direction: "horizontal"}) // focus pane 2
	// Format variables and the pane's environment; the last line shows.
	mustInput(t, s, protocol.Request{Op: "run-shell", Text: `echo first; echo "#{pane_target} pane=$BMUX_PANE"`})
	message("view:2 pane=2")
	// A pane other than the focused one, by ID.
	mustInput(t, s, protocol.Request{Op: "run-shell", PaneID: 1, Text: "echo pane=$BMUX_PANE"})
	message("pane=1")
	// A failure says so, with what it printed.
	mustInput(t, s, protocol.Request{Op: "run-shell", Text: "echo no makefile; exit 3"})
	message("run-shell failed (exit status 3): no makefile")
	if err := s.input(protocol.Request{Op: "run-shell", Text: "  "}); err == nil {
		t.Fatal("an empty command was accepted")
	}
}

func TestLastLineSkipsBlankLinesAndFitsTheStatusLine(t *testing.T) {
	for in, want := range map[string]string{
		"one\ntwo\n\n":       "two",
		"  spaced   out  \n": "spaced out",
		"":                   "",
	} {
		if got := lastLine([]byte(in)); got != want {
			t.Errorf("lastLine(%q) = %q, want %q", in, got, want)
		}
	}
	if got := lastLine([]byte(strings.Repeat("x", 300))); len([]rune(got)) != 201 || !strings.HasSuffix(got, "…") {
		t.Errorf("long line: %d runes", len([]rune(got)))
	}
}
