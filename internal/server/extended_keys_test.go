package server

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Shift+Enter reaches the program in the pane as plain Enter, unless the
// program asked for the kitty keyboard protocol first.
func TestShiftEnterReachesPaneProgram(t *testing.T) {
	shiftEnter := protocol.Request{Op: "key", PaneID: 1, Code: uv.KeyEnter, Mod: int(uv.ModShift)}
	hasAll := func(want ...string) func(*protocol.TerminalFrame) bool {
		return func(f *protocol.TerminalFrame) bool {
			text := frameText(f)
			for _, w := range want {
				if !strings.Contains(text, w) {
					return false
				}
			}
			return true
		}
	}

	t.Run("legacy program", func(t *testing.T) {
		s := editorNavigationSession(t)
		p := startTestEditor(t, s, 1)
		if err := s.input(shiftEnter); err != nil {
			t.Fatal(err)
		}
		awaitFrame(t, p, hasAll("CTRL_0d"))
	})

	t.Run("program that enabled the kitty keyboard protocol", func(t *testing.T) {
		s := editorNavigationSession(t)
		s.mu.Lock()
		p := s.panes[1]
		s.mu.Unlock()
		if err := s.input(protocol.Request{Op: "paste", PaneID: 1, Text: "printf '\\033[>1u'; echo KITTY_$((40+2))\n"}); err != nil {
			t.Fatal(err)
		}
		awaitFrame(t, p, hasAll("KITTY_42"))
		p = startTestEditor(t, s, 1)
		if err := s.input(shiftEnter); err != nil {
			t.Fatal(err)
		}
		// ESC [ 1 3 ; 2 u
		awaitFrame(t, p, hasAll("CTRL_1b", "CTRL_5b", "CTRL_31", "CTRL_33", "CTRL_3b", "CTRL_32", "CTRL_75"))
	})
}
