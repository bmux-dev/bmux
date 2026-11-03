package format

import (
	"strings"
	"testing"
)

// FuzzExpand expands formats from config files and scripts: it must never
// panic, and text without a # is returned as written.
func FuzzExpand(f *testing.F) {
	for _, seed := range []string{"#{session_name}:#{pane_id}", "## #{", "#{#{pane_id}}", "#{unknown} #", "plain text", "#{pane_current_path}/#{"} {
		f.Add(seed)
	}
	vars := map[string]string{"session_name": "work", "pane_id": "3", "pane_current_path": "/tmp"}
	f.Fuzz(func(t *testing.T, text string) {
		got := Expand(text, vars)
		if !strings.Contains(text, "#") && got != text {
			t.Fatalf("Expand(%q) = %q; text without # must stay as written", text, got)
		}
	})
}
