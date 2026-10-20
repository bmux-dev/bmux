package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

// A layout file (.bmux.toml in a project folder) declares a session's windows,
// their split trees and each pane's directory and startup command, like
// tmuxinator. It uses the restore snapshot's window and tree format, so
// creating a session in that folder builds it the way a restart rebuilds a
// saved session.
//
// A project's file can come from anyone who can write to the repository, so
// its commands run only once you trust that exact content (bmux layout trust,
// or bmux layout save, which writes it). Untrusted files still build their
// windows and panes; their commands are left out.

// LayoutFileName is the layout file looked for in a session's directory.
const LayoutFileName = ".bmux.toml"

const maxLayoutCommand = 8 << 10

type layoutFile struct {
	Active  int              `toml:"active,omitzero"` // 1-based window number
	Windows []windowSnapshot `toml:"windows"`
	Panes   []layoutPane     `toml:"panes"`
}

type layoutPane struct {
	ID      int    `toml:"id"`
	Dir     string `toml:"dir,omitempty"`     // relative to the project folder, or absolute, or ~/
	Command string `toml:"command,omitempty"` // typed into the pane's shell
}

const layoutHeader = `# bmux layout: creating a session in this folder builds these windows.
# Pane commands run only once the file is trusted: bmux layout trust.
# Directories are relative to this folder. Ratios are the first part's share
# in ten-thousandths; they default to an even split.

`

// parseLayout reads a layout file for a session named name in dir into a
// snapshot restoreSession can build. Every problem is an error: a file
// that's wrong should be fixed, not half-applied.
func parseLayout(data []byte, name, dir string) (sessionSnapshot, error) {
	var file layoutFile
	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return sessionSnapshot{}, err
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return sessionSnapshot{}, fmt.Errorf("unknown setting %q", undecoded[0].String())
	}
	if len(file.Windows) == 0 {
		return sessionSnapshot{}, fmt.Errorf("no [[windows]]")
	}
	if len(file.Panes) > maxPanes {
		return sessionSnapshot{}, fmt.Errorf("at most %d panes", maxPanes)
	}
	home, _ := os.UserHomeDir()
	snap := sessionSnapshot{Name: name, Dir: dir, Windows: file.Windows, Active: max(0, file.Active-1)}
	panes := make(map[int]paneSnapshot, len(file.Panes))
	for _, p := range file.Panes {
		if p.ID <= 0 || panes[p.ID].ID != 0 {
			return sessionSnapshot{}, fmt.Errorf("pane id %d must be a positive number used once", p.ID)
		}
		paneDir := dir
		switch {
		case p.Dir == "~" || strings.HasPrefix(p.Dir, "~/"):
			paneDir = filepath.Join(home, strings.TrimPrefix(p.Dir, "~"))
		case filepath.IsAbs(p.Dir):
			paneDir = p.Dir
		case p.Dir != "":
			paneDir = filepath.Join(dir, p.Dir)
		}
		if info, err := os.Stat(paneDir); err != nil || !info.IsDir() {
			return sessionSnapshot{}, fmt.Errorf("pane %d: %s is not a directory", p.ID, paneDir)
		}
		if len(p.Command) > maxLayoutCommand || strings.IndexFunc(p.Command, unicode.IsControl) >= 0 {
			return sessionSnapshot{}, fmt.Errorf("pane %d: commands are one line of at most %d bytes", p.ID, maxLayoutCommand)
		}
		entry := paneSnapshot{ID: p.ID, Dir: paneDir, Command: p.Command}
		panes[p.ID] = entry
		snap.Panes = append(snap.Panes, entry)
	}
	if snap.Active >= len(snap.Windows) {
		return sessionSnapshot{}, fmt.Errorf("active = %d, but there are %d windows", file.Active, len(snap.Windows))
	}
	seen := make(map[int]bool)
	for i, w := range snap.Windows {
		if err := validateWindowName(w.Name); err != nil {
			return sessionSnapshot{}, fmt.Errorf("window %d: %v", i+1, err)
		}
		evenRatios(w.Layout)
		if _, err := w.Layout.build(panes, seen); err != nil {
			return sessionSnapshot{}, fmt.Errorf("window %d: %v", i+1, err)
		}
	}
	for id := range panes {
		if !seen[id] {
			return sessionSnapshot{}, fmt.Errorf("pane %d isn't in any window", id)
		}
	}
	return snap, nil
}

// evenRatios fills in the ratios a hand-written file leaves out.
func evenRatios(l *layoutSnapshot) {
	if l == nil || l.Pane != 0 {
		return
	}
	if l.Ratio == 0 {
		l.Ratio = 5000
	}
	evenRatios(l.First)
	evenRatios(l.Second)
}

// encodeLayout writes a session snapshot as a layout file for dir: pane
// directories relative to it where they're inside it, and agents' resume
// commands as the panes' commands. commands are the commands of the file
// being replaced, by pane number; bmux can't tell what else a pane runs, so
// those are kept for panes without an agent.
func encodeLayout(snap sessionSnapshot, dir string, commands map[int]string) ([]byte, error) {
	file := layoutFile{Windows: snap.Windows}
	if snap.Active > 0 {
		file.Active = snap.Active + 1
	}
	base := canonicalPath(dir)
	for _, p := range snap.Panes {
		entry := layoutPane{ID: p.ID}
		rel, err := filepath.Rel(base, canonicalPath(p.Dir))
		switch {
		case err == nil && rel == ".": // the folder itself, the default
		case err == nil && rel != ".." && !strings.HasPrefix(rel, "../"):
			entry.Dir = rel
		default:
			entry.Dir = p.Dir
		}
		entry.Command = commands[p.ID]
		if len(p.Resume) > 0 {
			entry.Command = shellCommand(p.Resume)
		}
		file.Panes = append(file.Panes, entry)
	}
	var out bytes.Buffer
	out.WriteString(layoutHeader)
	encoder := toml.NewEncoder(&out)
	encoder.Indent = ""
	if err := encoder.Encode(file); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// canonicalPath resolves symlinks where it can (/tmp is /private/tmp on
// macOS), so one file or folder has one name in the trust store and in saved
// layouts.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// layoutTrust records the layout files whose current content may run
// commands: each file's absolute path and the SHA-256 of what was trusted.
// Editing a file untrusts it until it's trusted again, like direnv allow.
type layoutTrust struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

func layoutDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readLayoutTrust(path string) (layoutTrust, error) {
	trust := layoutTrust{Version: 1, Files: map[string]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return trust, nil
	}
	if err != nil {
		return trust, err
	}
	if err := json.Unmarshal(data, &trust); err != nil {
		return layoutTrust{Version: 1, Files: map[string]string{}}, fmt.Errorf("%s: %w", path, err)
	}
	if trust.Files == nil {
		trust.Files = map[string]string{}
	}
	return trust, nil
}

// layoutTrusted reports whether the layout file at file, holding data, was
// trusted in the store at trustPath. No store trusts nothing.
func layoutTrusted(trustPath, file string, data []byte) bool {
	if trustPath == "" {
		return false
	}
	trust, err := readLayoutTrust(trustPath)
	return err == nil && trust.Files[canonicalPath(file)] == layoutDigest(data)
}

// TrustLayout trusts the current content of dir's layout file, so creating a
// session there runs its commands. The store at trustPath is rewritten
// atomically.
func TrustLayout(trustPath, dir string) (string, error) {
	file, err := filepath.Abs(filepath.Join(dir, LayoutFileName))
	if err != nil {
		return "", err
	}
	file = canonicalPath(file)
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if _, err := parseLayout(data, "check", filepath.Dir(file)); err != nil {
		return "", fmt.Errorf("%s: %v", file, err)
	}
	trust, err := readLayoutTrust(trustPath)
	if err != nil {
		return "", err
	}
	trust.Files[file] = layoutDigest(data)
	encoded, err := json.MarshalIndent(trust, "", "  ")
	if err != nil {
		return "", err
	}
	return file, writeState(trustPath, append(encoded, '\n'))
}

// createSession starts a session in dir, built from dir's layout file if it
// has one. A file that doesn't parse gives one pane; that, or commands left
// out of an untrusted file, is the session's notice for its first client.
func (s *Server) createSession(name, dir string, width, height int) (*session, error) {
	file := filepath.Join(dir, LayoutFileName)
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return newSession(name, dir, s.shell, width, height)
	}
	notice := ""
	var snap sessionSnapshot
	if err == nil {
		snap, err = parseLayout(data, name, dir)
	}
	if err != nil {
		created, startErr := newSession(name, dir, s.shell, width, height)
		if created != nil {
			created.notice = fmt.Sprintf("%s: %v; started one pane instead", file, err)
		}
		return created, startErr
	}
	if !layoutTrusted(s.trustPath(), file, data) {
		commands := false
		for i := range snap.Panes {
			commands = commands || snap.Panes[i].Command != ""
			snap.Panes[i].Command = ""
		}
		if commands {
			notice = fmt.Sprintf("Built %s without running its commands: it isn't trusted. Read it, then run bmux layout trust in %s.", file, dir)
		}
	}
	created, err := restoreSession(snap, s.shell, width, height, map[string]bool{})
	if created != nil {
		created.notice = notice
	}
	return created, err
}

// SetLayoutTrust sets the store of trusted layout files; without one, layout
// files never run commands.
func (s *Server) SetLayoutTrust(path string) {
	s.layoutTrustPath.Store(&path)
}

func (s *Server) trustPath() string {
	if path := s.layoutTrustPath.Load(); path != nil {
		return *path
	}
	return ""
}

// layout returns the session's current windows as a layout file for dir,
// keeping the commands of the layout file already there.
func (s *session) layout(dir string) ([]byte, error) {
	snap, ok := s.persistentState()
	if !ok {
		return nil, fmt.Errorf("session has been terminated")
	}
	commands := make(map[int]string)
	if data, err := os.ReadFile(filepath.Join(dir, LayoutFileName)); err == nil {
		if old, err := parseLayout(data, "old", dir); err == nil {
			for _, p := range old.Panes {
				commands[p.ID] = p.Command
			}
		}
	}
	return encodeLayout(snap, dir, commands)
}

// takeNotice returns the session's notice once, for the first client to see.
func (s *session) takeNotice() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	notice := s.notice
	s.notice = ""
	return notice
}
