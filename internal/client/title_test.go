package client

import (
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestOuterTitleNamesSessionAndPaneTitle(t *testing.T) {
	s := focusTestScreen(t, 40, 10)
	u := &ui{screen: s, name: "api"}
	u.applySettings(config.Default())
	frame := func(title string) {
		u.applyFrame(&protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{
			{ID: 1, Width: 40, Height: 8, Terminal: &protocol.TerminalFrame{Title: title, Rows: [][]protocol.Cell{cells("x")}}},
		}})
		u.draw()
	}
	frame("")
	if got := s.GetTitle(); got != "api" {
		t.Fatalf("no pane title: %q", got)
	}
	frame("nvim README.md")
	if got := s.GetTitle(); got != "api · nvim README.md" {
		t.Fatalf("pane title: %q", got)
	}
	// An app's title can't smuggle escape sequences to the outer terminal.
	frame("evil\x1b\\\x1b]52;c;aGk=\x07\u009c done")
	if got := s.GetTitle(); strings.ContainsFunc(got, func(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }) || got != "api · evil\\]52;c;aGk= done" {
		t.Fatalf("control characters reached the title: %q", got)
	}
	frame(strings.Repeat("x", 500))
	if got := s.GetTitle(); len(got) != len("api · ")+maxTitleRunes {
		t.Fatalf("long title not capped: %d bytes", len(got))
	}
	u.picker = true
	u.draw()
	if got := s.GetTitle(); got != "bmux" {
		t.Fatalf("picker: %q", got)
	}

	off := config.Default()
	off.UI.SetTitle = false
	u.applySettings(off)
	u.picker = false
	frame("htop")
	if got := s.GetTitle(); got != "bmux" {
		t.Fatalf("set-title = false still changed the title: %q", got)
	}
}
