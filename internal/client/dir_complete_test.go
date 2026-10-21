package client

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v2"
	"strings"
)

func TestCompleteDir(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, filepath.Join(root, "docs"), filepath.Join(root, "app-web"), filepath.Join(root, "app-worker"), filepath.Join(root, "solo", "inner"), filepath.Join(root, ".hidden"))
	if err := os.WriteFile(filepath.Join(root, "apple.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "solo"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	tests := []struct {
		name, input, cwd, want string
		candidates             []string
	}{
		{"single match gets a slash", root + "/so", "/", root + "/solo/", nil},
		{"common prefix of several", root + "/ap", "/", root + "/app-w", []string{"app-web/", "app-worker/"}},
		{"files are not offered", root + "/apple", "/", root + "/apple", nil},
		{"no match leaves the text", root + "/zz", "/", root + "/zz", nil},
		{"hidden only after a dot", root + "/.", "/", root + "/.hidden/", nil},
		{"hidden skipped otherwise", root + "/h", "/", root + "/h", nil},
		{"symlinked directories count", root + "/li", "/", root + "/link/", nil},
		{"relative to cwd", "so", root, "solo/", nil},
		{"tilde keeps its form", "~/so", "/", "~/solo/", nil},
		{"bare tilde", "~", "/", "~/", nil},
		{"trailing slash lists children", root + "/", "/", root + "/", []string{"app-web/", "app-worker/", "docs/", "link/", "solo/"}},
		{"unreadable parent", "/nonexistent-dir/x", "/", "/nonexistent-dir/x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, candidates := completeDir(tt.input, tt.cwd)
			if got != tt.want || !reflect.DeepEqual(candidates, tt.candidates) {
				t.Fatalf("completeDir(%q) = %q, %q; want %q, %q", tt.input, got, candidates, tt.want, tt.candidates)
			}
		})
	}
}

func TestCreateFormTabCompletesOnlyTheDirectory(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, filepath.Join(root, "app-web"), filepath.Join(root, "app-worker"))
	u, _ := sidebarUI(t)
	u.cwd = root
	u.projects = projectPicker{open: true, creating: true, name: "x", dir: "app"}
	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)

	u.key(tab) // name field: back to the Projects tab
	if u.projects.creating {
		t.Fatal("Tab in the name field didn't leave the form")
	}
	u.projects.creating, u.projects.field = true, 1
	u.key(tab)
	if !u.projects.creating || u.projects.dir != "app-w" || u.projects.candidates != nil {
		t.Fatalf("first Tab: %+v", u.projects)
	}
	u.key(tab)
	if !reflect.DeepEqual(u.projects.candidates, []string{"app-web/", "app-worker/"}) {
		t.Fatalf("second Tab candidates: %q", u.projects.candidates)
	}
	u.draw()
	if text := screenText(u); !strings.Contains(text, "app-web/  app-worker/") {
		t.Fatalf("candidates not shown:\n%s", text)
	}
	u.key(runeKey('e'))
	if u.projects.dir != "app-we" || u.projects.candidates != nil {
		t.Fatalf("typing should drop the list: %+v", u.projects)
	}
	u.key(tab)
	if u.projects.dir != "app-web/" {
		t.Fatalf("dir %q", u.projects.dir)
	}
}
