package client

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestFindProjectsListsSessionsThenRepositories(t *testing.T) {
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
	settings := config.Projects{Roots: []string{"~/code", "~/missing"}, Depth: 2}
	got := findProjects(settings, home, sessions)
	want := []project{
		{dir: filepath.Join(code, "group", "web"), session: "web"},
		{dir: "/tmp", session: "scratch"},
		{dir: filepath.Join(code, "api")},
		{dir: filepath.Join(code, "worktree")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projects:\n got %+v\nwant %+v", got, want)
	}
	settings.Depth = 4
	if got := findProjects(settings, home, nil); len(got) != 4 {
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
	u.projects = projectPicker{open: true, list: []project{{dir: "/p/api", session: "api"}, {dir: "/p/web"}, {dir: "/p/worker"}}}
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
	if text := screenText(u); !strings.Contains(text, "Find: w") || !strings.Contains(text, "/p/web") {
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

func TestNewSessionPopupFindsOrCreates(t *testing.T) {
	special := func(k tcell.Key) *tcell.EventKey { return tcell.NewEventKey(k, 0, tcell.ModNone) }
	typeText := func(u *ui, text string) {
		for _, r := range text {
			u.key(runeKey(r))
		}
	}
	setup := func(t *testing.T) (*ui, <-chan protocol.Request) {
		u, _ := sidebarUI(t)
		socket, requests := fakeStartupRPC(t, func(protocol.Request) protocol.Response { return protocol.Response{} })
		u.socket, u.cwd = socket, "/work"
		u.projects = projectPicker{open: true, list: []project{{dir: "/p/api", session: "api"}, {dir: "/p/web"}}, sessions: []protocol.Session{{Name: "api"}, {Name: "web"}}}
		return u, requests
	}
	t.Run("Tab prefills from the highlighted project", func(t *testing.T) {
		u, _ := setup(t)
		u.key(special(tcell.KeyDown))
		u.key(special(tcell.KeyTab))
		if !u.projects.creating || u.projects.field != 0 || u.projects.name != "" || u.projects.dir != "/p/web" {
			t.Fatalf("form: %+v", u.projects)
		}
		u.key(special(tcell.KeyEscape)) // Tab in the directory field completes it instead
		if u.projects.creating || u.projects.query != "" || u.projects.chosen != 1 {
			t.Fatalf("back on projects: %+v", u.projects)
		}
	})
	t.Run("form lists the directory first and hints at the empty name", func(t *testing.T) {
		u, _ := setup(t)
		u.key(special(tcell.KeyTab))
		u.draw()
		text := screenText(u)
		dir, name := strings.Index(text, "Start directory"), strings.Index(text, "Session name")
		if dir < 0 || name < 0 || dir > name || !strings.Contains(text, "(named after the directory)") {
			t.Fatalf("form:\n%s", text)
		}
	})
	t.Run("no match creates from the filter", func(t *testing.T) {
		u, _ := setup(t)
		typeText(u, "zz")
		u.key(special(tcell.KeyEnter))
		if !u.projects.creating || u.projects.name != "zz" || u.projects.dir != "/work" {
			t.Fatalf("form: %+v", u.projects)
		}
	})
	t.Run("empty name is named after the directory", func(t *testing.T) {
		u, requests := setup(t)
		u.key(special(tcell.KeyTab)) // api is highlighted and its session exists
		u.key(special(tcell.KeyEnter))
		if got := <-requests; got.Op != "create" || got.Name != "api-2" || got.Dir != "/p/api" {
			t.Fatalf("request %+v", got)
		}
	})
	t.Run("invalid name is rejected without a request", func(t *testing.T) {
		u, requests := setup(t)
		u.key(special(tcell.KeyTab))
		u.key(special(tcell.KeyDown))
		typeText(u, "a b")
		u.key(special(tcell.KeyEnter))
		if u.projects.formErr == "" || u.projects.field != 1 || !u.projects.open {
			t.Fatalf("form: %+v", u.projects)
		}
		select {
		case r := <-requests:
			t.Fatalf("unexpected request %+v", r)
		case <-time.After(50 * time.Millisecond):
		}
	})
	t.Run("submit creates and attaches", func(t *testing.T) {
		u, requests := setup(t)
		u.key(special(tcell.KeyTab))
		u.key(special(tcell.KeyDown))
		u.key(special(tcell.KeyCtrlU))
		typeText(u, "scratch")
		u.key(special(tcell.KeyUp))
		u.key(special(tcell.KeyCtrlU))
		typeText(u, "sub")
		u.key(special(tcell.KeyEnter))
		for _, want := range []protocol.Request{{Op: "create", Name: "scratch", Dir: "/work/sub"}, {Op: "attach", Name: "scratch"}} {
			select {
			case got := <-requests:
				if got.Op != want.Op || got.Name != want.Name || got.Dir != want.Dir {
					t.Fatalf("request %+v, want %+v", got, want)
				}
			case <-time.After(time.Second):
				t.Fatalf("no %s request", want.Op)
			}
		}
		if u.name != "scratch" || u.projects.open {
			t.Fatalf("name %q, popup open %v", u.name, u.projects.open)
		}
	})
	t.Run("create directory toggle is on by default and sent with create", func(t *testing.T) {
		u, requests := setup(t)
		u.key(special(tcell.KeyTab))
		if !u.projects.makeDir {
			t.Fatal("toggle should default on")
		}
		u.key(special(tcell.KeyEnter))
		if got := <-requests; got.Op != "create" || !got.MakeDir {
			t.Fatalf("request %+v", got)
		}
	})
	t.Run("space on the toggle turns it off and the request says so", func(t *testing.T) {
		u, requests := setup(t)
		u.key(special(tcell.KeyTab))
		u.key(special(tcell.KeyDown))
		u.key(special(tcell.KeyDown))
		if u.projects.field != 2 {
			t.Fatalf("field %d", u.projects.field)
		}
		u.key(special(tcell.KeyDown)) // stops at the toggle
		if u.projects.field != 2 {
			t.Fatalf("field %d past the toggle", u.projects.field)
		}
		typeText(u, "x") // other keys leave the toggle and the inputs alone
		name, dir := u.projects.name, u.projects.dir
		u.key(runeKey(' '))
		if u.projects.makeDir || u.projects.name != name || u.projects.dir != dir {
			t.Fatalf("form: %+v", u.projects)
		}
		u.draw()
		if text := screenText(u); !strings.Contains(text, "[ ] Create directory if it doesn't exist") {
			t.Fatalf("toggle not drawn off:\n%s", text)
		}
		u.key(special(tcell.KeyRight))
		if !u.projects.makeDir {
			t.Fatal("Right should toggle back on")
		}
		u.key(special(tcell.KeyLeft))
		u.key(special(tcell.KeyEnter))
		if got := <-requests; got.Op != "create" || got.MakeDir {
			t.Fatalf("request %+v", got)
		}
	})
	t.Run("start-dir replaces the working directory", func(t *testing.T) {
		u, requests := setup(t)
		u.settings.Projects.StartDir = "/home/me"
		typeText(u, "zz")
		u.key(special(tcell.KeyEnter))
		if u.projects.dir != "/home/me" || u.projects.name != "zz" {
			t.Fatalf("form: %+v", u.projects)
		}
		u.key(special(tcell.KeyCtrlU))
		typeText(u, "sub")
		u.key(special(tcell.KeyEnter))
		if got := <-requests; got.Op != "create" || got.Name != "zz" || got.Dir != "/home/me/sub" {
			t.Fatalf("request %+v", got)
		}
	})
	t.Run("start-dir wins over the highlighted project", func(t *testing.T) {
		u, _ := setup(t)
		u.settings.Projects.StartDir = "/home/me"
		u.key(special(tcell.KeyDown)) // web is highlighted
		u.key(special(tcell.KeyTab))
		if !u.projects.creating || u.projects.dir != "/home/me" || u.projects.name != "" {
			t.Fatalf("form: %+v", u.projects)
		}
		u.key(special(tcell.KeyEscape))
		typeText(u, "we") // still matches web; the filter names the session
		u.key(special(tcell.KeyTab))
		if u.projects.dir != "/home/me" || u.projects.name != "we" {
			t.Fatalf("form with a filter: %+v", u.projects)
		}
	})
	t.Run("popup is centered at 80 percent", func(t *testing.T) {
		u, _ := setup(t)
		u.draw()
		if text, _, _ := u.screen.Get(10, 2); text != "┌" {
			t.Fatalf("top-left corner %q", text)
		}
		if text, _, _ := u.screen.Get(89, 17); text != "┘" {
			t.Fatalf("bottom-right corner %q", text)
		}
		if row := pickerScreenLine(u, 2); !strings.Contains(row, "Projects") || !strings.Contains(row, "Create") {
			t.Fatalf("tabs row %q", row)
		}
		if !strings.Contains(pickerScreenLine(u, 19), "Tab form") {
			t.Fatalf("footer %q", pickerScreenLine(u, 19))
		}
	})
}
