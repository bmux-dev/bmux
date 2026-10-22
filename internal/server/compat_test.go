package server

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// releaseAtLeast reports whether release ("v0.4.0") is version or newer.
func releaseAtLeast(t *testing.T, release, version string) bool {
	t.Helper()
	parse := func(v string) [3]int {
		var n [3]int
		for i, part := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
			var err error
			if n[i], err = strconv.Atoi(part); err != nil {
				t.Fatalf("version %q", v)
			}
		}
		return n
	}
	a, b := parse(release), parse(version)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

// Each release's server wrote testdata/releases/TAG/sessions.json for a
// session "work" in a project folder: pane 1 split side by side, the new pane
// split stacked, pane 3 moved into sub/, and from v0.4.0 on a second window
// opened from pane 3 (scripts/compat-fixtures.sh). Every one must restore.
func TestEveryReleasedSaveFileRestores(t *testing.T) {
	fixtures, err := filepath.Glob("../../testdata/releases/*/sessions.json")
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no release fixtures: %v", err)
	}
	for _, fixture := range fixtures {
		release := filepath.Base(filepath.Dir(fixture))
		t.Run(release, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			proj := realDir(t, t.TempDir())
			if err := os.Mkdir(filepath.Join(proj, "sub"), 0700); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			saved := strings.NewReplacer("@PROJ@", proj, "@HOME@", home).Replace(string(data))
			path := filepath.Join(t.TempDir(), "sessions.json")
			if err := os.WriteFile(path, []byte(saved), 0600); err != nil {
				t.Fatal(err)
			}
			srv := persistentServer(t, path)
			t.Cleanup(srv.Close)

			s := srv.sessions["work"]
			if s == nil {
				t.Fatalf("session work not restored from %s's save file", release)
			}
			s.mu.Lock()
			var windows [][]int
			for _, w := range s.windows {
				var ids []int
				w.root.leaves(&ids)
				windows = append(windows, ids)
			}
			first := s.windows[0]
			s.mu.Unlock()
			want := [][]int{{1, 2, 3}}
			if releaseAtLeast(t, release, "v0.4.0") {
				want = append(want, []int{4}) // windows began in v0.4.0
			}
			if !reflect.DeepEqual(windows, want) {
				t.Fatalf("windows and panes %v, want %v", windows, want)
			}
			if first.focused != 3 || first.root.direction != "horizontal" || first.root.second.direction != "vertical" {
				t.Fatalf("layout: focused %d, split %q then %q", first.focused, first.root.direction, first.root.second.direction)
			}
			// Each pane's shell starts where its predecessor's was.
			for id, dir := range map[int]string{1: proj, 2: proj, 3: filepath.Join(proj, "sub")} {
				awaitCwd(t, s.panes[id], dir)
			}
		})
	}
}
