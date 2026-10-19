package server

import (
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Apps see focus (?1004) as "this pane is focused and an attached client's
// terminal has focus", the same state that suppresses attention.
func TestAppsHearFocusOnlyWhileTheirPaneIsViewed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("focus", t.TempDir(), "/bin/sh", 120, 20)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	input := func(r protocol.Request) {
		t.Helper()
		if err := s.input(r); err != nil {
			t.Fatal(err)
		}
	}
	reports := func(want string) {
		t.Helper()
		awaitFrame(t, s.panes[1], func(f *protocol.TerminalFrame) bool {
			return strings.Contains(frameText(f), want)
		})
		if f, _ := s.panes[1].frame(0); strings.Contains(frameText(f), want+"^[[") {
			t.Fatalf("unexpected focus report after %q:\n%s", want, frameText(f))
		}
	}
	s.setViewing(true)
	// cat -v prints each report: ^[[I for focus in, ^[[O for focus out.
	input(protocol.Request{Op: "paste", PaneID: 1, Text: "stty -icanon -echo; printf '\\033[?1004h'; exec cat -v\n"})
	reports("^[[I") // turning focus events on while viewed reports focus at once

	input(protocol.Request{Op: "split", PaneID: 1, Direction: "horizontal"})
	reports("^[[I^[[O")
	input(protocol.Request{Op: "select-pane", PaneID: 1})
	reports("^[[I^[[O^[[I")

	// A second focused client leaving keeps the pane focused; the last one doesn't.
	s.setViewing(true)
	s.setViewing(false)
	s.setViewing(false)
	reports("^[[I^[[O^[[I^[[O")
}
