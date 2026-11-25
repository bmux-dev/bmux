package format

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/bmux-dev/bmux/internal/protocol"
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

func TestExpandShellQuotesByContext(t *testing.T) {
	vars := map[string]string{
		"session_name": "api",
		"pane_id":      "7",
		"pane_title":   "evil'; touch /tmp/pwn; echo '",
		"window_name":  `$(id) "quoted" \back\` + "`date`",
		"pane_path":    "/srv/repo with space",
		"bare_path":    "/srv/api-1",
	}
	for _, tc := range []struct {
		name, in, want string
	}{
		{"bare target unquoted", "echo #{session_name}:#{pane_id}", "echo api:7"},
		{"bare path unquoted", "cd #{bare_path}", "cd /srv/api-1"},
		{"unquoted payload single-quoted", "echo #{pane_title}", "echo 'evil'\\''; touch /tmp/pwn; echo '\\'''"},
		{"unquoted space path", "cd #{pane_path}", "cd '/srv/repo with space'"},
		{"inside single quotes", "echo '#{pane_title}'", "echo 'evil'\\''; touch /tmp/pwn; echo '\\'''"},
		{"inside double quotes", `echo "#{window_name}"`, "echo \"\\$(id) \\\"quoted\\\" \\\\back\\\\\\`date\\`\""},
		{"inside command substitution", `x=$(printf '%s' #{pane_title}); printf '%s' "$x"`, "x=$(printf '%s' 'evil'\\''; touch /tmp/pwn; echo '\\'''); printf '%s' \"$x\""},
		{"escaped quote stays unquoted", `echo \'#{pane_path}`, `echo \''/srv/repo with space'`},
		{"nul stripped", "echo #{nul}", "echo "},
	} {
		if tc.name == "nul stripped" {
			vars["nul"] = "a\x00b"
			tc.want = "echo ab"
		}
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandShell(tc.in, vars); got != tc.want {
				t.Fatalf("ExpandShell(%q) =\n  %q\nwant\n  %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExpandShellBlocksExecutionInRealSh(t *testing.T) {
	payload := "a' b\" $(touch /tmp/bmux_never_1) `touch /tmp/bmux_never_2`; id | cat & \nend"
	vars := map[string]string{"pane_title": payload}
	for _, tmpl := range []string{
		"printf '%s' #{pane_title}",
		"printf '%s' '#{pane_title}'",
		`printf '%s' "#{pane_title}"`,
		`out=$(printf '%s' #{pane_title}); printf '%s' "$out"`,
		`out=$(printf '%s' "#{pane_title}"); printf '%s' "$out"`,
	} {
		cmd := ExpandShell(tmpl, vars)
		out, err := exec.Command("/bin/sh", "-c", cmd).Output()
		if err != nil {
			t.Fatalf("%s failed (%q): %v", tmpl, cmd, err)
		}
		if string(out) != payload {
			t.Fatalf("%s (%q) produced %q, want %q", tmpl, cmd, string(out), payload)
		}
	}
}

func TestEnvAndApplyEnv(t *testing.T) {
	info := protocol.PaneInfo{
		Session: "api", SessionID: 3, SessionPath: "/srv/api\x00x",
		Window: 2, WindowName: "dev;id", Pane: 7,
		Path: "/srv/api/web", Command: "nvim", Title: "title'x", Agent: "claude", State: "blocked",
	}
	env := ApplyEnv([]string{"PATH=/usr/bin", "BMUX_PANE_TITLE=stale", "BMUX_PANE_AGENT=old"}, info)
	for _, want := range []string{
		"PATH=/usr/bin",
		"BMUX_SESSION_PATH=/srv/apix",
		"BMUX_WINDOW_INDEX=2",
		"BMUX_WINDOW_NAME=dev;id",
		"BMUX_PANE_TARGET=api:7",
		"BMUX_PANE_PATH=/srv/api/web",
		"BMUX_PANE_COMMAND=nvim",
		"BMUX_PANE_TITLE=title'x",
		"BMUX_PANE_AGENT=claude",
		"BMUX_PANE_AGENT_STATE=blocked",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("ApplyEnv missing %q", want)
		}
	}
	for _, e := range env {
		if strings.HasPrefix(e, "BMUX_PANE_TITLE=stale") || strings.HasPrefix(e, "BMUX_PANE_AGENT=old") {
			t.Errorf("stale env entry leaked: %q", e)
		}
	}
}
