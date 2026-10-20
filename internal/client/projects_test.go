package client

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindProjectsListsSessionsZoxideThenRepositories(t *testing.T) {
	home := t.TempDir()
	code := filepath.Join(home, "code")
	mkdirs(t,
		filepath.Join(code, "api", ".git"),
		filepath.Join(code, "api", "sub", ".git"), // inside a repository: not listed
		filepath.Join(code, "group", "web", ".git"),
		filepath.Join(code, "group", "deep", "deeper", "tool", ".git"), // below depth
		filepath.Join(code, ".hidden", "x", ".git"),
		filepath.Join(code, "node_modules", "pkg", ".git"),
		filepath.Join(code, "notes"),
	)
	// A worktree's .git is a file.
	mkdirs(t, filepath.Join(code, "worktree"))
	if err := os.WriteFile(filepath.Join(code, "worktree", ".git"), []byte("gitdir: x"), 0600); err != nil {
		t.Fatal(err)
	}
	sessions := []protocol.Session{{Name: "web", Dir: filepath.Join(code, "group", "web")}, {Name: "scratch", Dir: "/tmp"}}
	zoxide := []string{"/srv/site", filepath.Join(code, "api")}
	settings := config.Projects{Roots: []string{"~/code", "~/missing"}, Depth: 2}
	got := findProjects(settings, home, sessions, zoxide)
	want := []project{
		{filepath.Join(code, "group", "web"), "web"},
		{"/tmp", "scratch"},
		{"/srv/site", ""},
		{filepath.Join(code, "api"), ""},
		{filepath.Join(code, "worktree"), ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projects:\n got %+v\nwant %+v", got, want)
	}
	settings.Depth = 4
	if got := findProjects(settings, home, nil, nil); len(got) != 4 {
		t.Fatalf("depth 4 should reach the deeper tool: %+v", got)
	}
}

func TestProjectSessionReusesTheFoldersSessionOrCreatesOne(t *testing.T) {
	sessions := []protocol.Session{{Name: "api-old", Dir: "/elsewhere"}, {Name: "api", Dir: "/projects/api"}}
	socket, requests := fakeStartupRPC(t, func(r protocol.Request) protocol.Response {
		return protocol.Response{Sessions: sessions}
	})
	if name, err := ProjectSession(socket, "/projects/api/"); err != nil || name != "api" {
		t.Fatalf("existing: %q %v", name, err)
	}
	if r := <-requests; r.Op != "list" {
		t.Fatalf("lookup %+v", r)
	}
	if name, err := ProjectSession(socket, "/projects/web"); err != nil || name != "web" {
		t.Fatalf("new: %q %v", name, err)
	}
	<-requests
	if r := <-requests; r.Op != "create" || r.Name != "web" || r.Dir != "/projects/web" {
		t.Fatalf("create %+v", r)
	}
}

func TestProjectPickerFiltersAsYouTypeAndLeaves(t *testing.T) {
	u, requests := sidebarUI(t)
	u.projects = projectPicker{open: true, list: []project{{"/p/api", "api"}, {"/p/web", ""}, {"/p/worker", ""}}}
	for _, r := range "wk" {
		u.key(runeKey(r))
	}
	if got := u.filteredProjects(); len(got) != 1 || got[0].dir != "/p/worker" {
		t.Fatalf("filter wk: %+v", got)
	}
	u.key(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	u.key(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if got := u.filteredProjects(); len(got) != 2 || u.projects.chosen != 1 {
		t.Fatalf("filter w: %+v chosen %d", got, u.projects.chosen)
	}
	u.draw()
	if text := screenText(u); !strings.Contains(text, "Projects / w") || !strings.Contains(text, "/p/web") {
		t.Fatalf("screen:\n%s", text)
	}
	noRequest(t, requests) // typing never reaches the pane
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if u.projects.open || u.quitting {
		t.Fatal("Esc should close the picker and stay attached")
	}
	// Opened by bmux project, with nothing attached, Esc leaves bmux.
	u.conn = nil
	u.projects = projectPicker{open: true}
	u.key(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if !u.quitting {
		t.Fatal("Esc without a session didn't exit")
	}
}
