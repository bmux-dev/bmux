package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestPaneShellActsAsTheTargetPane(t *testing.T) {
	// Run from pane 4 of session work, for pane 7 of session api.
	t.Setenv("BMUX_SESSION", "work")
	t.Setenv("BMUX_SESSION_ID", "1")
	t.Setenv("BMUX_PANE", "4")
	dir := t.TempDir()
	cmd := paneShell(protocol.PaneInfo{Session: "api", SessionID: 9, Pane: 7, Path: dir, SessionPath: "/missing"}, "echo #{pane_target}")
	if cmd.Dir != dir || cmd.Args[len(cmd.Args)-1] != "echo api:7" {
		t.Fatalf("dir %q, args %q", cmd.Dir, cmd.Args)
	}
	for _, want := range []string{"BMUX_SESSION=api", "BMUX_SESSION_ID=9", "BMUX_PANE=7"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "BMUX_PANE=4") || strings.HasPrefix(entry, "BMUX_SESSION=work") {
			t.Errorf("the calling pane's %s leaked through", entry)
		}
	}
	// Without a readable directory it runs where bmux was started.
	if cmd := paneShell(protocol.PaneInfo{Path: "/missing", SessionPath: "/also-missing"}, "true"); cmd.Dir != "" {
		t.Fatalf("dir %q for missing directories", cmd.Dir)
	}
}
