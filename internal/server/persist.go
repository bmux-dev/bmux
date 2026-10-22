package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Sessions survive server restarts and reboots as layouts, not processes:
// a restored pane is a fresh login shell in the directory its shell was in.
// Agents come back through a resume command, a new process that reopens the
// agent's own conversation. Nothing else that was running is revived.

const (
	stateVersion      = 1
	stateSaveInterval = 5 * time.Second
)

type stateFile struct {
	Version  int               `json:"version"`
	Sessions []sessionSnapshot `json:"sessions"`
}

type sessionSnapshot struct {
	Name    string           `json:"name"`
	Dir     string           `json:"dir"`
	Created time.Time        `json:"created"`
	Used    time.Time        `json:"used"`
	Windows []windowSnapshot `json:"windows"`
	Active  int              `json:"active,omitempty"` // index into Windows
	Panes   []paneSnapshot   `json:"panes"`
	// Before windows (v0.3 and earlier) a session saved one layout. These are
	// read as its only window and never written.
	Focused int             `json:"focused,omitempty"`
	Zoomed  bool            `json:"zoomed,omitempty"`
	Layout  *layoutSnapshot `json:"layout,omitempty"`
}

// Windows and layout trees are also the format of layout files (.bmux.toml),
// hence the TOML names.
type windowSnapshot struct {
	Name    string          `json:"name,omitempty" toml:"name,omitempty"`
	Focused int             `json:"focused" toml:"focused,omitzero"`
	Zoomed  bool            `json:"zoomed,omitempty" toml:"zoomed,omitempty"`
	Layout  *layoutSnapshot `json:"layout" toml:"layout"`
}

// A leaf names a pane; an inner node has a direction, ratio and two children.
type layoutSnapshot struct {
	Pane      int             `json:"pane,omitempty" toml:"pane,omitzero"`
	Direction string          `json:"direction,omitempty" toml:"direction,omitempty"`
	Ratio     int             `json:"ratio,omitempty" toml:"ratio,omitzero"`
	First     *layoutSnapshot `json:"first,omitempty" toml:"first,omitempty"`
	Second    *layoutSnapshot `json:"second,omitempty" toml:"second,omitempty"`
}

type paneSnapshot struct {
	ID    int    `json:"id"`
	Dir   string `json:"dir"`
	Agent string `json:"agent,omitempty"`
	// Resume reopens the agent's conversation. Reported commands are exact;
	// built-in ones continue the agent's latest conversation in Dir.
	Resume   []string `json:"resume,omitempty"`
	Reported bool     `json:"reported,omitempty"`
	// Command is a layout file's startup line, typed into the shell as is.
	// Layout files only; never saved.
	Command string `json:"-"`
}

// Built-in resume commands for recognized agents without a reported one.
var agentContinue = map[string][]string{
	"claude":   {"claude", "--continue"},
	"codex":    {"codex", "resume", "--last"},
	"omp":      {"omp", "--continue"},
	"opencode": {"opencode", "--continue"},
	"pi":       {"pi", "--continue"},
}

func snapshotLayout(n *layout) *layoutSnapshot {
	if n == nil {
		return nil
	}
	if n.id != 0 {
		return &layoutSnapshot{Pane: n.id}
	}
	return &layoutSnapshot{Direction: n.direction, Ratio: n.ratio, First: snapshotLayout(n.first), Second: snapshotLayout(n.second)}
}

// build validates a saved tree: every leaf is a distinct listed pane.
func (l *layoutSnapshot) build(panes map[int]paneSnapshot, seen map[int]bool) (*layout, error) {
	if l == nil {
		return nil, fmt.Errorf("missing layout node")
	}
	if l.Pane != 0 {
		if _, ok := panes[l.Pane]; !ok || l.Pane < 0 || seen[l.Pane] || l.First != nil || l.Second != nil {
			return nil, fmt.Errorf("invalid layout pane %d", l.Pane)
		}
		seen[l.Pane] = true
		return &layout{id: l.Pane}, nil
	}
	if (l.Direction != "horizontal" && l.Direction != "vertical") || l.Ratio <= 0 || l.Ratio >= 10000 {
		return nil, fmt.Errorf("invalid layout split")
	}
	first, err := l.First.build(panes, seen)
	if err != nil {
		return nil, err
	}
	second, err := l.Second.build(panes, seen)
	if err != nil {
		return nil, err
	}
	return &layout{direction: l.Direction, ratio: l.Ratio, first: first, second: second}, nil
}

// persistentState captures layout under session.mu, then reads directories from the
// OS and agent state under each pane.mu, never holding both kinds of lock
// while the OS is queried.
func (s *session) persistentState() (sessionSnapshot, bool) {
	s.mu.Lock()
	if s.stopped || s.stopping.Load() || len(s.windows) == 0 {
		s.mu.Unlock()
		return sessionSnapshot{}, false
	}
	snap := sessionSnapshot{Name: s.info.Name, Dir: s.info.Dir, Created: s.info.Created, Used: s.info.Used, Active: s.active}
	var ids []int
	for _, w := range s.windows {
		snap.Windows = append(snap.Windows, windowSnapshot{Name: w.name, Focused: w.focused, Zoomed: w.zoomed, Layout: snapshotLayout(w.root)})
		w.root.leaves(&ids)
	}
	panes := make([]*pane, len(ids))
	for i, id := range ids {
		panes[i] = s.panes[id]
	}
	s.mu.Unlock()
	for i, p := range panes {
		entry := paneSnapshot{ID: ids[i], Dir: snap.Dir}
		if p == nil {
			snap.Panes = append(snap.Panes, entry)
			continue
		}
		if dir, err := processCwd(p.cmd.Process.Pid); err == nil && dir != "" {
			entry.Dir = dir
		}
		p.mu.Lock()
		detected := p.detected
		entry.Agent = p.agent
		if entry.Agent == "" {
			entry.Agent = detected
		}
		if len(p.resume) > 0 {
			entry.Resume, entry.Reported = append([]string(nil), p.resume...), true
		} else if argv, ok := currentSettings().resumeFor(detected); ok {
			entry.Resume = append([]string(nil), argv...)
		}
		p.mu.Unlock()
		// An agent's conversations belong to its own directory, which can
		// differ from the shell's: restore the pane there. The foreground
		// group's leader is the job the shell started.
		if detected != "" {
			if group, err := foregroundPTYGroup(p.pty); err == nil && group > 0 {
				if dir, err := processCwd(group); err == nil && dir != "" {
					entry.Dir = dir
				}
			}
		}
		snap.Panes = append(snap.Panes, entry)
	}
	return snap, true
}

func (s *Server) captureState() stateFile {
	s.mu.Lock()
	items := make([]*session, 0, len(s.sessions))
	for _, item := range s.sessions {
		items = append(items, item)
	}
	s.mu.Unlock()
	state := stateFile{Version: stateVersion, Sessions: []sessionSnapshot{}}
	for _, item := range items {
		if snap, ok := item.persistentState(); ok {
			state.Sessions = append(state.Sessions, snap)
		}
	}
	sort.Slice(state.Sessions, func(i, j int) bool {
		if !state.Sessions[i].Created.Equal(state.Sessions[j].Created) {
			return state.Sessions[i].Created.Before(state.Sessions[j].Created)
		}
		return state.Sessions[i].Name < state.Sessions[j].Name
	})
	return state
}

func shellQuote(arg string) string {
	if arg != "" && strings.Trim(arg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./=:,@+%") == "" {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func shellCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func existingDir(dir, fallback string) string {
	if info, err := os.Stat(dir); err == nil && info.IsDir() && filepath.IsAbs(dir) {
		return dir
	}
	return fallback
}

// restoreWindows validates saved windows against the saved panes. Each pane
// may appear in only one window; a window whose tree is invalid is dropped
// whole, and the active index follows the windows that remain.
func restoreWindows(saved []windowSnapshot, active int, panes map[int]paneSnapshot) ([]*window, int) {
	seen := make(map[int]bool)
	var windows []*window
	restoredActive := 0
	for i, snap := range saved {
		local := maps.Clone(seen)
		root, err := snap.Layout.build(panes, local)
		if err != nil {
			continue
		}
		seen = local
		w := &window{root: root, focused: snap.Focused}
		if validateWindowName(snap.Name) == nil {
			w.name = snap.Name
		}
		if !root.contains(w.focused) {
			w.focused = root.firstPane()
		}
		w.zoomed = snap.Zoomed && root.id == 0
		if i == active {
			restoredActive = len(windows)
		}
		windows = append(windows, w)
	}
	return windows, restoredActive
}

// restoreSession rebuilds saved windows with fresh shells at width x height
// (0 for the default size). A window with an invalid tree is dropped; if none
// is left, the session gets one pane in its directory. resumed tracks
// built-in "continue latest" commands per directory so two panes never
// reopen the same conversation; reported commands are exact and always run,
// and so are layout files' commands.
func restoreSession(snap sessionSnapshot, shell string, width, height int, resumed map[string]bool) (*session, error) {
	home, _ := os.UserHomeDir()
	dir := existingDir(snap.Dir, home)
	panes := make(map[int]paneSnapshot, len(snap.Panes))
	for _, p := range snap.Panes {
		panes[p.ID] = p
	}
	saved := snap.Windows
	if len(saved) == 0 && snap.Layout != nil {
		saved = []windowSnapshot{{Focused: snap.Focused, Zoomed: snap.Zoomed, Layout: snap.Layout}}
	}
	windows, active := restoreWindows(saved, snap.Active, panes)
	if len(windows) == 0 {
		windows, active, panes = []*window{{root: &layout{id: 1}, focused: 1}}, 0, map[int]paneSnapshot{1: {ID: 1, Dir: dir}}
	}
	w, h := dimensions(width, height)
	used := snap.Used
	if used.IsZero() {
		used = time.Now()
	}
	s := &session{
		id:    sessionIDs.Add(1),
		info:  protocol.Session{Name: snap.Name, Dir: dir, Created: time.Now(), Used: used, Restored: true},
		panes: make(map[int]*pane), windows: windows, active: active, width: w, height: h, shell: shell,
		revision: 1, done: make(chan struct{}), owned: make(map[*pane]struct{}),
	}
	type started struct {
		p    *pane
		snap paneSnapshot
		dir  string
	}
	var created []started
	for _, win := range windows {
		// Every pane, even under a zoom; resizePanes fits them afterwards.
		var positions []placement
		win.root.place(rect{0, 0, w, h}, &positions)
		for _, pos := range positions {
			saved := panes[pos.id]
			paneDir := existingDir(saved.Dir, dir)
			p, err := newPane(s.id, snap.Name, paneDir, shell, max(2, pos.w), max(1, pos.h), pos.id)
			if err != nil {
				for _, c := range created {
					c.p.stop()
				}
				return nil, err
			}
			s.panes[pos.id] = p
			s.owned[p] = struct{}{}
			s.nextID = max(s.nextID, pos.id+1)
			created = append(created, started{p, saved, paneDir})
		}
	}
	s.info.Panes = len(s.panes)
	for id, p := range s.panes {
		s.watchPane(id, p)
	}
	go s.watchAgents()
	s.mu.Lock()
	_ = s.resizePanes()
	s.mu.Unlock()
	for _, c := range created {
		if c.snap.Command != "" {
			_ = c.p.input(protocol.Request{Op: "type", Text: c.snap.Command + "\r"})
			continue
		}
		if len(c.snap.Resume) == 0 || validateResume(c.snap.Resume) != nil {
			continue
		}
		if !c.snap.Reported {
			key := c.dir + "\x00" + strings.Join(c.snap.Resume, "\x00")
			if resumed[key] {
				continue
			}
			resumed[key] = true
		}
		// Typed ahead before the shell has started: its line editor reads it
		// as the first command once ready.
		_ = c.p.input(protocol.Request{Op: "type", Text: shellCommand(c.snap.Resume) + "\r"})
	}
	return s, nil
}

func loadState(path string) (stateFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return stateFile{Version: stateVersion}, nil
	}
	if err != nil {
		return stateFile{}, err
	}
	var state stateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return stateFile{}, fmt.Errorf("unreadable saved sessions: %w", err)
	}
	if state.Version != stateVersion {
		return stateFile{}, fmt.Errorf("saved sessions use format %d; this bmux reads format %d", state.Version, stateVersion)
	}
	return state, nil
}

func writeState(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sessions-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// EnablePersistence restores the sessions saved at path, then saves the
// current sessions every few seconds and once more when the server closes.
// Call it before Serve. An unreadable file is moved aside, never overwritten
// silently, and the server starts empty.
func (s *Server) EnablePersistence(path string) error {
	state, err := loadState(path)
	if err != nil {
		aside := path + ".unreadable"
		if renameErr := os.Rename(path, aside); renameErr != nil {
			return fmt.Errorf("%w; also could not move it aside: %v", err, renameErr)
		}
		fmt.Fprintf(os.Stderr, "bmux: %v; moved to %s\n", err, aside)
		state = stateFile{Version: stateVersion}
	}
	resumed := make(map[string]bool)
	if !currentSettings().restore {
		state.Sessions = nil // restore = false: start fresh, keep the file
	}
	for _, snap := range state.Sessions {
		if validateName(snap.Name) != nil {
			continue
		}
		item, err := restoreSession(snap, s.shell, 0, 0, resumed)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bmux: could not restore session %s: %v\n", snap.Name, err)
			continue
		}
		s.mu.Lock()
		if s.closed || s.sessions[snap.Name] != nil {
			s.mu.Unlock()
			item.stop()
			continue
		}
		s.adoptLocked(snap.Name, item)
		s.mu.Unlock()
	}
	s.persistMu.Lock()
	s.persistPath = path
	s.persistStop = make(chan struct{})
	s.persistMu.Unlock()
	go s.saveLoop(s.persistStop)
	return nil
}

func (s *Server) saveLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(stateSaveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := s.saveState(); err != nil {
				fmt.Fprintf(os.Stderr, "bmux: saving sessions: %v\n", err)
			}
		}
	}
}

// saveState writes the current sessions when they changed since the last
// write. After the final save at Close it never writes again, so shutting
// sessions down doesn't erase what will be restored.
func (s *Server) saveState() error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.persistPath == "" || s.persistFinal || !currentSettings().restore {
		return nil
	}
	data, err := json.MarshalIndent(s.captureState(), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if bytes.Equal(data, s.persistLast) {
		return nil
	}
	if err := writeState(s.persistPath, data); err != nil {
		return err
	}
	s.persistLast = data
	return nil
}

// finishPersistence saves one last time and stops the saver.
func (s *Server) finishPersistence() {
	if err := s.saveState(); err != nil {
		fmt.Fprintf(os.Stderr, "bmux: saving sessions: %v\n", err)
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.persistStop != nil && !s.persistFinal {
		close(s.persistStop)
	}
	s.persistFinal = true
}
