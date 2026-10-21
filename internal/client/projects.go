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

// The new-session popup (Ctrl-a n, bmux project) has two tabs. Projects lists
// project folders and lands in the session already started there, or
// creates one named after the folder, like sesh and tmux-sessionizer. Create
// is a name and directory form for a session anywhere else.

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
	sessions []protocol.Session
	err      error
}

type projectPicker struct {
	open       bool
	loading    bool
	query      string
	chosen     int
	list       []project
	sessions   []protocol.Session // every session, for unique name suggestions
	err        string             // loading/opening error; hides the list
	creating   bool               // Create tab shown instead of Projects
	field      int                // focused form field: 0 directory, 1 name, 2 create-directory toggle
	name, dir  string             // form inputs
	makeDir    bool               // build the directory when it doesn't exist
	formErr    string             // create validation/RPC error, shown in the form
	candidates []string           // directories Tab could not choose between
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
			result.sessions = r.Sessions
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
	u.projects.sessions = result.sessions
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
	if u.picker {
		u.requestPreview()
	}
}

// formBase is what the Create tab's relative paths are relative to and where
// it starts: projects.start-dir, else the directory bmux was started in (or
// the highlighted project's, see beginProjectForm).
func (u *ui) formBase() string {
	if dir := u.settings.Projects.StartDir; dir != "" {
		return startDirectory(dir, u.cwd)
	}
	return u.cwd
}

// beginProjectForm switches to the Create tab with the directory focused.
// The directory is projects.start-dir if set, else the highlighted project's,
// else the working directory. The name starts empty, because it is usually
// the directory's name and createFromForm fills that in; only text typed as a
// filter that wasn't used to pick a project becomes the name.
func (u *ui) beginProjectForm() {
	u.projects.creating, u.projects.field, u.projects.formErr, u.projects.candidates = true, 0, "", nil
	u.projects.makeDir = true
	matches := u.filteredProjects()
	base := u.formBase()
	name := ""
	if u.settings.Projects.StartDir == "" && u.projects.chosen < len(matches) {
		base = matches[u.projects.chosen].dir
	} else {
		name = strings.TrimSpace(u.projects.query)
	}
	u.projects.dir = projectLabel(base)
	u.projects.name = name
}

// createFromForm creates the session the Create tab describes and attaches
// to it. Failures stay in the form.
func (u *ui) createFromForm() {
	dir := startDirectory(u.projects.dir, u.formBase())
	name := strings.TrimSpace(u.projects.name)
	if name == "" {
		name = freeDirectorySessionName(dir, u.projects.sessions)
	}
	if err := validatePromptName(name); err != nil {
		u.projects.formErr, u.projects.field = err.Error(), 1
		return
	}
	w, h := u.screen.Size()
	if _, err := Request(u.socket, protocol.Request{Op: "create", Name: name, Dir: dir, MakeDir: u.projects.makeDir, Width: u.paneViewportWidth(w), Height: paneViewportHeight(h)}); err != nil {
		u.projects.formErr = err.Error()
		return
	}
	u.projects = projectPicker{}
	if err := u.switchTo(name); err != nil {
		u.notice(err.Error())
	}
}

// startDirectory resolves a typed start directory: ~ is the home directory
// and relative paths are relative to cwd.
func startDirectory(input, cwd string) string {
	dir := strings.TrimSpace(input)
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	return dir
}

// projectsKey drives the popup: the Projects tab filters as you type, the
// Create tab edits the name and directory. Tab switches between them.
func (u *ui) projectsKey(e *tcell.EventKey) {
	if u.projects.creating {
		u.projectFormKey(e)
		return
	}
	matches := u.filteredProjects()
	move := func(delta int) {
		u.projects.chosen = max(0, min(u.projects.chosen+delta, len(matches)-1))
	}
	switch e.Key() {
	case tcell.KeyEscape:
		u.closeProjects()
	case tcell.KeyCtrlC:
		u.quitting = true
	case tcell.KeyTab:
		u.beginProjectForm()
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
		} else if !u.projects.loading {
			u.beginProjectForm()
		}
	case tcell.KeyRune:
		if e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			u.projects.query += string(e.Rune())
			u.projects.chosen = 0
		}
	}
}

func (u *ui) projectFormKey(e *tcell.EventKey) {
	// text is the focused field's input; nil on the toggle.
	var text *string
	switch u.projects.field {
	case 0:
		text = &u.projects.dir
	case 1:
		text = &u.projects.name
	}
	if e.Key() != tcell.KeyTab {
		u.projects.candidates = nil // the list belongs to the Tab that showed it
	}
	switch e.Key() {
	case tcell.KeyTab:
		if u.projects.field == 0 {
			u.completeProjectDir()
		} else {
			u.projects.creating, u.projects.formErr = false, ""
		}
	case tcell.KeyEscape:
		u.projects.creating, u.projects.formErr = false, ""
	case tcell.KeyCtrlC:
		u.quitting = true
	case tcell.KeyDown, tcell.KeyCtrlJ:
		u.projects.field = min(u.projects.field+1, 2)
	case tcell.KeyUp, tcell.KeyCtrlK:
		u.projects.field = max(u.projects.field-1, 0)
	case tcell.KeyLeft, tcell.KeyRight:
		if text == nil {
			u.projects.makeDir = !u.projects.makeDir
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlH:
		if text != nil {
			*text, u.projects.formErr = dropRune(*text), ""
		}
	case tcell.KeyCtrlU:
		if text != nil {
			*text, u.projects.formErr = "", ""
		}
	case tcell.KeyEnter:
		u.createFromForm()
	case tcell.KeyRune:
		if e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			break
		}
		switch {
		case text != nil:
			*text += string(e.Rune())
			u.projects.formErr = ""
		case e.Rune() == ' ':
			u.projects.makeDir = !u.projects.makeDir
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

// projectsFooter is the key hint line for the popup's current tab.
func (u *ui) projectsFooter() string {
	if u.projects.creating {
		switch u.projects.field {
		case 0:
			return "New session: ↑↓ field · Tab completes directory · Enter create · Esc back to projects"
		case 2:
			return "New session: ↑↓ field · Space toggles · Enter create · Tab/Esc back to projects"
		}
		return "New session: ↑↓ field · Enter create · empty name uses the directory's · Ctrl-u clear · Tab/Esc back to projects"
	}
	return "New session: type to find · ↑↓ select · Enter open or create · Tab form · Esc close"
}

// drawProjects draws the popup, 80% of the screen and centered between the
// bars, over whatever is behind it.
func (u *ui) drawProjects(w, h int) {
	u.dimBody(w, h)
	u.screen.HideCursor()
	if w < 20 || h < 8 {
		return
	}
	bw := min(w, max(20, w*4/5))
	bh := min(h-2, max(6, h*4/5))
	x, y := (w-bw)/2, 1+(h-2-bh)/2
	ix, iy, iw, ih := x+2, y+1, bw-4, bh-2
	for row := range bh {
		u.screen.PutStrStyled(x, y+row, fit("", bw), uiBaseStyle)
	}
	u.screen.PutStrStyled(x, y, "┌"+strings.Repeat("─", bw-2)+"┐", uiAccentStyle)
	u.screen.PutStrStyled(x, y+bh-1, "└"+strings.Repeat("─", bw-2)+"┘", uiAccentStyle)
	for row := 1; row < bh-1; row++ {
		u.screen.Put(x, y+row, "│", uiAccentStyle)
		u.screen.Put(x+bw-1, y+row, "│", uiAccentStyle)
	}
	projectsTab, createTab := uiAccentStyle.Reverse(true), uiMutedStyle
	if u.projects.creating {
		projectsTab, createTab = createTab, projectsTab
	}
	tabsEnd := x + bw - 1
	u.screen.PutStrStyled(x+2, y, fit(" Projects ", min(10, max(0, tabsEnd-(x+2)))), projectsTab)
	u.screen.PutStrStyled(x+12, y, fit(" Create ", min(8, max(0, tabsEnd-(x+12)))), createTab)
	if u.projects.creating {
		u.drawProjectForm(ix, iy, iw, ih)
	} else {
		u.drawProjectList(ix, iy, iw, ih)
	}
}

func (u *ui) drawProjectList(ix, iy, iw, ih int) {
	find := "Find: " + tailFit(u.projects.query, iw-7)
	u.screen.PutStrStyled(ix, iy, fit(find, iw), uiBaseStyle)
	u.screen.ShowCursor(ix+displayWidth(find), iy)
	u.screen.PutStrStyled(ix, iy+1, strings.Repeat("─", iw), uiMutedStyle)
	matches := u.filteredProjects()
	u.projects.chosen = max(0, min(u.projects.chosen, len(matches)-1))
	rows := max(0, ih-2)
	listY := iy + 2
	message := ""
	switch {
	case u.projects.err != "":
		message = "Error: " + u.projects.err
	case u.projects.loading:
		message = "Finding projects…"
	case len(u.projects.list) == 0:
		message = "No projects. Set projects.roots in the config file, or install zoxide."
	case len(matches) == 0:
		message = "No matches. Enter creates a session named after the filter."
	}
	if message != "" && rows > 0 {
		u.screen.PutStrStyled(ix, listY, fit(message, iw), uiMutedStyle)
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
			style = uiBaseStyle.Background(selectionBackground).Foreground(selectionForeground).Bold(true)
		}
		y := listY + i - start
		u.screen.PutStrStyled(ix, y, fit(label, iw), style)
		if note != "" && displayWidth(label)+displayWidth(note) < iw {
			u.screen.PutStrStyled(ix+iw-displayWidth(note), y, note, style.Foreground(accentColor))
		}
	}
}

func (u *ui) drawProjectForm(ix, iy, iw, ih int) {
	put := func(row int, text string, style tcell.Style) {
		if row < ih {
			u.screen.PutStrStyled(ix, iy+row, fit(text, iw), style)
		}
	}
	labelStyle := func(field int) tcell.Style {
		if u.projects.field == field {
			return uiAccentStyle
		}
		return uiMutedStyle
	}
	name, dir := tailFit(u.projects.name, iw-1), tailFit(u.projects.dir, iw-1)
	put(0, "Start directory", labelStyle(0))
	put(1, dir, uiBaseStyle)
	put(3, "Session name", labelStyle(1))
	if name == "" {
		put(4, "(named after the directory)", uiMutedStyle)
	} else {
		put(4, name, uiBaseStyle)
	}
	box := "[ ]"
	if u.projects.makeDir {
		box = "[x]"
	}
	put(6, box+" Create directory if it doesn't exist", labelStyle(2))
	next := 8
	if u.projects.formErr != "" {
		put(8, "Error: "+u.projects.formErr, uiBaseStyle.Bold(true))
		next = 9
	}
	if lines := wrapNames(u.projects.candidates, iw); len(lines) > 0 {
		rows := max(0, ih-next)
		if len(lines) > rows && rows > 0 {
			lines = append(lines[:rows-1], "…")
		}
		for i, line := range lines {
			put(next+i, line, uiMutedStyle)
		}
	}
	if u.projects.field == 2 {
		u.screen.HideCursor()
		return
	}
	row, value := 1, dir
	if u.projects.field == 1 {
		row, value = 4, name
	}
	if row < ih {
		u.screen.ShowCursor(ix+displayWidth(value), iy+row)
	}
}
