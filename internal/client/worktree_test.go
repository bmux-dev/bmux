package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// worktreeFixture is a repository under code/ and two worktrees that live
// elsewhere, one per ticket, each in a folder named repo.
type worktreeFixture struct {
	home, main, one, two string
}

func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := realPath(t.TempDir())
	f := worktreeFixture{
		home: home,
		main: filepath.Join(home, "code", "stl"),
		one:  filepath.Join(home, "workspaces", "STL-1", "repo"),
		two:  filepath.Join(home, "workspaces", "STL-2", "repo"),
	}
	mkdirs(t, f.main)
	runGit(t, f.main, "init", "-b", "main")
	runGit(t, f.main, "commit", "--allow-empty", "-m", "init")
	runGit(t, f.main, "worktree", "add", "-b", "feat/one", f.one)
	runGit(t, f.main, "worktree", "add", "-b", "feat/two", f.two)
	return f
}

func TestReadRepoFindsTheMainRepositoryFromAWorktreeAnywhere(t *testing.T) {
	f := newWorktreeFixture(t)
	sub := filepath.Join(f.one, "pkg")
	mkdirs(t, sub)
	got := readRepo(sub)
	want := repoInfo{Top: f.one, Root: f.main, Branch: "feat/one", Worktree: true}
	if got != want {
		t.Fatalf("worktree subdirectory: got %+v want %+v", got, want)
	}
	if got := readRepo(f.main); got != (repoInfo{Top: f.main, Root: f.main, Branch: "main"}) {
		t.Fatalf("main repository: %+v", got)
	}
	if got := readRepo(f.home); got != (repoInfo{}) {
		t.Fatalf("outside a repository: %+v", got)
	}
	if got := worktreeDirs(f.main); len(got) != 2 || got[0] != f.one || got[1] != f.two {
		t.Fatalf("worktrees: %v", got)
	}
	if err := os.RemoveAll(f.two); err != nil {
		t.Fatal(err)
	}
	if got := worktreeDirs(f.main); len(got) != 1 || got[0] != f.one {
		t.Fatalf("a deleted worktree is still listed: %v", got)
	}
}

func TestReadRepoHandlesHandWrittenLayouts(t *testing.T) {
	dir := realPath(t.TempDir())
	write := func(path, text string) {
		t.Helper()
		mkdirs(t, filepath.Dir(path))
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// A bare repository's worktree, with the relative paths newer git can write.
	bare := filepath.Join(dir, "repo.git")
	write(filepath.Join(bare, "worktrees", "w", "commondir"), "../..\n")
	write(filepath.Join(bare, "worktrees", "w", "HEAD"), "ref: refs/heads/topic\n")
	wt := filepath.Join(dir, "w")
	write(filepath.Join(wt, ".git"), "gitdir: ../repo.git/worktrees/w\n")
	if got := readRepo(wt); got != (repoInfo{Top: wt, Root: bare, Branch: "topic", Worktree: true}) {
		t.Fatalf("bare: %+v", got)
	}
	if projectName(bare) != "repo" {
		t.Fatalf("bare repository is titled %q", projectName(bare))
	}
	// A submodule's gitdir has no commondir: it is a project of its own.
	sub := filepath.Join(dir, "super", "sub")
	write(filepath.Join(dir, "super", ".git", "modules", "sub", "HEAD"), "0123456789abcdef0123456789abcdef01234567\n")
	write(filepath.Join(sub, ".git"), "gitdir: ../.git/modules/sub\n")
	if got := readRepo(sub); got != (repoInfo{Top: sub, Root: sub, Branch: "0123456"}) {
		t.Fatalf("submodule: %+v", got)
	}
	// A .git file that names nothing is not a repository.
	broken := filepath.Join(dir, "broken")
	write(filepath.Join(broken, ".git"), "not a gitdir file")
	if got := readRepo(broken); got != (repoInfo{}) {
		t.Fatalf("broken: %+v", got)
	}
}

func TestGroupLinesPutsWorktreesUnderTheirProject(t *testing.T) {
	infos := map[string]repoInfo{
		"/code/stl":       {Top: "/code/stl", Root: "/code/stl", Branch: "main"},
		"/w/1/repo":       {Top: "/w/1/repo", Root: "/code/stl", Branch: "feat/1", Worktree: true},
		"/w/2/repo":       {Top: "/w/2/repo", Root: "/code/stl", Branch: "feat/2", Worktree: true},
		"/code/solo":      {Top: "/code/solo", Root: "/code/solo", Branch: "main"},
		"/w/9/repo":       {Top: "/w/9/repo", Root: "/code/other", Branch: "fix", Worktree: true},
		"/code/stl/inner": {Top: "/code/stl", Root: "/code/stl", Branch: "main"},
	}
	info := func(dir string) repoInfo { return infos[dir] }
	sessions := []protocol.Session{
		{Name: "one", Dir: "/w/1/repo"},
		{Name: "notes", Dir: "/notes"},
		{Name: "solo", Dir: "/code/solo"},
		{Name: "orphan", Dir: "/w/9/repo"},
		{Name: "stl", Dir: "/code/stl/inner"},
		{Name: "two", Dir: "/w/2/repo"},
	}
	var got []string
	for _, l := range groupLines(sessions, info) {
		entry := l.session.Name
		if l.header != "" {
			entry = "[" + l.header + "]"
		} else if l.nested {
			entry = "  " + entry + "@" + l.branch
		}
		got = append(got, entry)
	}
	want := []string{
		"[stl]", "  one@feat/1", "  stl@main", "  two@feat/2",
		"notes", "solo",
		"[other]", "  orphan@fix",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("lines:\n got %v\nwant %v", got, want)
	}
	for i, l := range groupLines(sessions, info) {
		wantIndex := map[string]int{"one": 0, "stl": 1, "two": 2, "notes": 3, "solo": 4, "orphan": 5}[l.session.Name]
		if (l.header != "") != (l.index < 0) || (l.header == "" && l.index != wantIndex) {
			t.Fatalf("line %d %+v has index %d, want %d", i, l, l.index, wantIndex)
		}
	}
}

func TestSidebarGroupsWorktreeSessionsAndNavigationSkipsHeaders(t *testing.T) {
	f := newWorktreeFixture(t)
	u, _ := sidebarUI(t)
	now := time.Now()
	u.sessions = []protocol.Session{
		{Name: "two", Dir: f.two, Created: now.Add(3 * time.Second)},
		{Name: "scratch", Dir: f.home, Created: now.Add(2 * time.Second)},
		{Name: "one", Dir: f.one, Created: now.Add(time.Second)},
	}
	u.name = "scratch"
	rows, count := u.sidebarRows()
	var got []string
	for _, r := range rows[:count] {
		entry := r.session.Name
		if r.isHeader() {
			entry = "[" + r.header + "]"
		} else if r.nested {
			entry += "@" + r.branch
		}
		got = append(got, entry)
	}
	if want := "[stl],one@feat/one,two@feat/two,scratch"; strings.Join(got, ",") != want {
		t.Fatalf("rows %v, want %s", got, want)
	}
	// Cycling follows the grouped order and never lands on a header.
	if next := adjacentSession(u.sessions, "scratch", 1); next != "one" {
		t.Fatalf("after scratch: %q", next)
	}
	if prev := adjacentSession(u.sessions, "one", -1); prev != "scratch" {
		t.Fatalf("before one: %q", prev)
	}
	u.sidebarFocus, u.sidebarSelected = true, sidebarTarget{name: "scratch"}
	u.moveSidebar(-1)
	if u.sidebarSelected.name != "two" {
		t.Fatalf("up from scratch: %+v", u.sidebarSelected)
	}
	u.moveSidebar(-1)
	u.moveSidebar(-1) // the header above one is skipped, and the top stays put
	if u.sidebarSelected.name != "one" {
		t.Fatalf("up past the top: %+v", u.sidebarSelected)
	}
	// Clicking the header selects nothing.
	u.sidebarSelected = sidebarTarget{name: "scratch"}
	u.sidebarFocus = false
	u.sidebarClick(paneOriginY+1, 20)
	if u.sidebarFocus || u.sidebarSelected.name != "scratch" {
		t.Fatalf("header click changed selection: focus %v %+v", u.sidebarFocus, u.sidebarSelected)
	}
	u.draw()
	screen := pickerScreenText(u)
	for _, want := range []string{" stl", "     one", "feat/one"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("sidebar missing %q:\n%s", want, screen)
		}
	}
}

func TestPickerGroupsWorktreeSessionsUnlessSearching(t *testing.T) {
	f := newWorktreeFixture(t)
	u := pickerUI(t)
	u.sessions = []protocol.Session{
		{Name: "scratch", Dir: f.home},
		{Name: "two", Dir: f.two},
		{Name: "other", Dir: f.home},
		{Name: "one", Dir: f.one},
	}
	var names []string
	for _, s := range u.filtered() {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "scratch,two,one,other" {
		t.Fatalf("grouped order %s", got)
	}
	u.chosen = 2
	u.drawPicker(100, 24)
	screen := pickerScreenText(u)
	if !strings.Contains(screen, "     stl") || !strings.Contains(screen, "(2) + ") || !strings.Contains(screen, "one [feat/one]") {
		t.Fatalf("picker missing header, numbering or branch:\n%s", screen)
	}
	// A click on the header chooses nothing; a click on a session chooses it.
	list, _ := pickerAreas(100, 24)
	u.chosen = 0
	u.pickerMouse(tcell.NewEventMouse(0, 0, tcell.Button1, 0), 3, list.y+1) // the header line
	if u.chosen != 0 {
		t.Fatalf("header click chose %d", u.chosen)
	}
	u.pickerMouse(tcell.NewEventMouse(0, 0, tcell.Button1, 0), 3, list.y+3) // "one"
	if u.chosen != 2 {
		t.Fatalf("session click chose %d", u.chosen)
	}
	u.query = "o"
	for _, s := range u.filtered() {
		if lines, _ := u.pickerLines(u.filtered(), 10); len(lines) != len(u.filtered()) || s.Name == "" {
			t.Fatalf("searching must not add headers: %+v", lines)
		}
	}
}

func TestFindProjectsNestsWorktreesOutsideTheRoots(t *testing.T) {
	f := newWorktreeFixture(t)
	settings := config.Projects{Roots: []string{"~/code"}, Depth: 2}
	got := findProjects(settings, f.home, []protocol.Session{{Name: "ticket", Dir: f.two}})
	want := []project{
		{dir: f.main, branch: "main"},
		{dir: f.two, session: "ticket", nested: true, branch: "feat/two"}, // sessions are listed first
		{dir: f.one, nested: true, branch: "feat/one"},
	}
	if len(got) != len(want) {
		t.Fatalf("projects %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("project %d: got %+v want %+v", i, got[i], want[i])
		}
	}
	// A repository nobody has a worktree of stays plain.
	solo := filepath.Join(f.home, "code", "solo")
	mkdirs(t, solo)
	runGit(t, solo, "init", "-b", "main")
	for _, p := range findProjects(settings, f.home, nil) {
		if p.dir == solo && (p.nested || p.branch != "") {
			t.Fatalf("solo repository decorated: %+v", p)
		}
	}
}

func TestWorktreeSessionNamesTellTicketFoldersApart(t *testing.T) {
	f := newWorktreeFixture(t)
	if got := directorySessionBase(f.one); got != "stl-STL-1" {
		t.Fatalf("ambiguous worktree folder: %q", got)
	}
	if got := directorySessionBase(f.main); got != "stl" {
		t.Fatalf("main repository: %q", got)
	}
	distinct := filepath.Join(f.home, "workspaces", "stl-hotfix")
	runGit(t, f.main, "worktree", "add", "-b", "hotfix", distinct)
	if got := directorySessionBase(distinct); got != "stl-hotfix" {
		t.Fatalf("distinctive folder keeps its name: %q", got)
	}
	sub := filepath.Join(f.one, "pkg")
	mkdirs(t, sub)
	if got := directorySessionBase(sub); got != "pkg" {
		t.Fatalf("subdirectory keeps its name: %q", got)
	}
}
