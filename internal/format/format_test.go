package format

import (
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestExpandReplacesVariablesAndKeepsEverythingElse(t *testing.T) {
	vars := map[string]string{"session_name": "api", "pane_id": "7"}
	for in, want := range map[string]string{
		"#{session_name}:#{pane_id}": "api:7",
		"#{nope} left":               " left", // unknown names expand to nothing
		"## and #x":                  "# and #x",
		"100% #":                     "100% #",
		"#{session_name":             "#{session_name", // unterminated, kept
		"":                           "",
		"#{}":                        "",
	} {
		if got := Expand(in, vars); got != want {
			t.Errorf("Expand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVarsCoverEveryDocumentedName(t *testing.T) {
	info := protocol.PaneInfo{
		Session: "api", SessionID: 3, SessionPath: "/srv/api", SessionWindows: 2, SessionAttached: 1,
		Window: 2, WindowName: "logs", WindowActive: true, WindowZoomed: false, WindowPanes: 3,
		Pane: 7, Index: 2, Active: true, Path: "/srv/api/web", Command: "nvim", Title: "notes",
		Width: 80, Height: 24, Agent: "claude", State: "idle",
	}
	vars := Vars(info)
	for _, name := range Names {
		if _, ok := vars[name]; !ok {
			t.Errorf("%s has no value", name)
		}
	}
	if len(vars) != len(Names) {
		t.Errorf("Vars has names Names doesn't list: %d vs %d", len(vars), len(Names))
	}
	got := Expand("#{pane_target} #{window_index}.#{pane_index} #{window_active}#{window_zoomed} #{pane_width}x#{pane_height} #{pane_current_command} #{pane_agent_state}", vars)
	if want := "api:7 2.2 10 80x24 nvim idle"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
