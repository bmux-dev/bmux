package client

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

// The project picker (Ctrl-a f, bmux project) lists project folders and
// lands in the session already started there, or creates one named after
// the folder, like sesh and tmux-sessionizer.

// Bounds keep a huge root from stalling the picker.
const (
	maxProjects       = 2000
	maxProjectVisits  = 20000
	zoxideTimeout     = 2 * time.Second
	projectWalkBudget = 3 * time.Second
)

type project struct {
	dir     string
	session string // the most recently used session started here, or ""
}

type projectResult struct {
	projects []project
	err      error
}

type projectPicker struct {
	open    bool
	loading bool
	query   string
	chosen  int
	list    []project
	err     string
}

// openProjects shows the picker and finds projects off the UI goroutine.
func (u *ui) openProjects() {
	u.projects = projectPicker{open: true, loading: true}
	u.prefix, u.sidebarFocus = false, false
	if u.projectResults == nil {
		u.projectResults = make(chan projectResult, 1)
	}
	socket, settings, results, done := u.socket, u.settings.Projects, u.projectResults, u.done
	go func() {
		r, err := Request(socket, protocol.Request{Op: "list"})
		result := projectResult{err: err}
		if err == nil {
			home, _ := os.UserHomeDir()
			result.projects = findProjects(settings, home, r.Sessions, zoxideDirs(settings))
		}
		select {
		case results <- result:
		case <-done:
		}
	}()
}

func (u *ui) finishProjects(result projectResult) {
	if !u.projects.open {
		return
	}
	u.projects.loading = false
	u.projects.list = result.projects
	if result.err != nil {
		u.projects.err = result.err.Error()
	}
}

// findProjects lists folders with a session first (most recent first), then
// zoxide's directories in its ranking, then the git repositories under the
// roots in path order. Each folder appears once.
func findProjects(settings config.Projects, home string, sessions []protocol.Session, zoxide []string) []project {
	var result []project
	seen := make(map[string]bool)
	add := func(dir, session string) {
		dir = filepath.Clean(dir)
		key := dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			key = resolved // one folder reached through a symlink is listed once
		}
		if seen[key] || len(result) >= maxProjects {
			return
		}
		seen[key] = true
		result = append(result, project{dir, session})
	}
	for _, s := range sessions {
		if s.Dir != "" {
			add(s.Dir, s.Name)
		}
	}
	for _, dir := range zoxide {
		add(dir, sessionIn(sessions, dir))
	}
	deadline := time.Now().Add(projectWalkBudget)
	visits := 0
	for _, root := range settings.Roots {
		if r, ok := strings.CutPrefix(root, "~"); ok {
			root = filepath.Join(home, r)
		}
		var found []string
		walkProjects(root, settings.Depth, &visits, deadline, &found)
		sort.Strings(found)
		for _, dir := range found {
			add(dir, sessionIn(sessions, dir))
		}
	}
	return result
}

// walkProjects collects git repositories at or below dir, depth levels down,
// without descending into a repository or hidden and dependency folders.
func walkProjects(dir string, depth int, visits *int, deadline time.Time, found *[]string) {
	*visits++
	if *visits > maxProjectVisits || len(*found) >= maxProjects || time.Now().After(deadline) {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // missing or unreadable roots are skipped
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			*found = append(*found, dir)
			return
		}
	}
	if depth == 0 {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		walkProjects(filepath.Join(dir, name), depth-1, visits, deadline, found)
	}
}

// zoxideDirs is zoxide's directory list, best first, or nil when zoxide is
// off, missing or slow.
func zoxideDirs(settings config.Projects) []string {
	if !settings.Zoxide {
		return nil
	}
	path, err := exec.LookPath("zoxide")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), zoxideTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "query", "--list").Output()
	if err != nil {
		return nil
	}
	var dirs []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if dir := strings.TrimSpace(scanner.Text()); filepath.IsAbs(dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// projectLabel writes dir with ~ for the home directory.
func projectLabel(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(dir, home); ok && (rest == "" || rest[0] == filepath.Separator) {
			return "~" + rest
		}
	}
	return dir
}

// filteredProjects applies the fuzzy filter, best match first.
func (u *ui) filteredProjects() []project {
	if u.projects.query == "" {
		return u.projects.list
	}
	type match struct {
		item  project
		score int
	}
	var matches []match
	for _, p := range u.projects.list {
		if score, ok := fuzzyScore(u.projects.query, projectLabel(p.dir)); ok {
			matches = append(matches, match{p, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := make([]project, len(matches))
	for i, m := range matches {
		result[i] = m.item
	}
	return result
}

// closeProjects leaves the picker: back to the attached session, or out of
// bmux when the picker was all there was (bmux project).
func (u *ui) closeProjects() {
	u.projects = projectPicker{}
	if u.conn == nil && !u.picker {
		u.quitting = true
	}
}

// projectsKey filters as you type; arrows and Ctrl-j/k/n/p move.
func (u *ui) projectsKey(e *tcell.EventKey) {
	matches := u.filteredProjects()
	move := func(delta int) {
		u.projects.chosen = max(0, min(u.projects.chosen+delta, len(matches)-1))
	}
	switch e.Key() {
	case tcell.KeyEscape:
		u.closeProjects()
	case tcell.KeyCtrlC:
		u.quitting = true
	case tcell.KeyDown, tcell.KeyCtrlJ, tcell.KeyCtrlN:
		move(1)
	case tcell.KeyUp, tcell.KeyCtrlK, tcell.KeyCtrlP:
		move(-1)
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlH:
		u.projects.query, u.projects.chosen = dropRune(u.projects.query), 0
	case tcell.KeyCtrlU:
		u.projects.query, u.projects.chosen = "", 0
	case tcell.KeyEnter:
		if u.projects.chosen < len(matches) {
			u.openProject(matches[u.projects.chosen].dir)
		}
	case tcell.KeyRune:
		if e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			u.projects.query += string(e.Rune())
			u.projects.chosen = 0
		}
	}
}

// openProject attaches to dir's session, creating it if there is none.
func (u *ui) openProject(dir string) {
	name, err := ProjectSession(u.socket, dir)
	if err != nil {
		u.projects.err = err.Error()
		return
	}
	u.projects = projectPicker{}
	if err := u.switchTo(name); err != nil {
		u.notice(err.Error())
	}
}

func (u *ui) drawProjects(w, h int) {
	u.screen.PutStrStyled(0, 0, fit(" Projects / "+u.projects.query, w), barStyle.Bold(true))
	u.screen.ShowCursor(min(w-1, displayWidth(" Projects / "+u.projects.query)), 0)
	matches := u.filteredProjects()
	u.projects.chosen = max(0, min(u.projects.chosen, len(matches)-1))
	rows := max(0, h-2)
	message := ""
	switch {
	case u.projects.err != "":
		message = "Error: " + u.projects.err
	case u.projects.loading:
		message = "Finding projects…"
	case len(u.projects.list) == 0:
		message = "No projects. Set projects.roots in the config file, or install zoxide."
	case len(matches) == 0:
		message = "No matches. Backspace edits the filter."
	}
	if message != "" && rows > 0 {
		u.screen.PutStrStyled(1, 1, fit(message, max(0, w-2)), uiMutedStyle)
		if u.projects.err != "" || len(matches) == 0 {
			return
		}
	}
	start := max(0, u.projects.chosen-rows+1)
	for i := start; i < len(matches) && i-start < rows; i++ {
		p := matches[i]
		label, note := " "+projectLabel(p.dir), ""
		if p.session != "" {
			note = " session " + p.session + " "
		}
		style := uiBaseStyle
		if i == u.projects.chosen {
			style = uiBaseStyle.Background(selectionBackground).Foreground(selectionForeground).Bold(true).Reverse(true)
		}
		y := 1 + i - start
		u.screen.PutStrStyled(0, y, fit(label, w), style)
		if note != "" && displayWidth(label)+displayWidth(note) < w {
			u.screen.PutStrStyled(w-displayWidth(note), y, note, style.Foreground(accentColor))
		}
	}
}
