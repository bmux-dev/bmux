package client

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/josephferrero/bmux/internal/protocol"
)

// CreateDirectorySession creates a session in cwd named after it, for bare
// bmux. It always creates fresh work, never reattaching to a similarly named
// session; the server's create operation arbitrates concurrent name
// collisions.
func CreateDirectorySession(socket, cwd string) (string, error) {
	r, err := Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return "", err
	}
	return createDirectorySession(socket, cwd, r.Sessions)
}

// ProjectSession returns the most recently used session started in dir, or
// creates one there named after it, for the new-session popup (Ctrl-a n, bmux project).
func ProjectSession(socket, dir string) (string, error) {
	r, err := Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return "", err
	}
	if name := sessionIn(r.Sessions, dir); name != "" {
		return name, nil
	}
	return createDirectorySession(socket, dir, r.Sessions)
}

// sessionIn is the first session, in the server's recency order, that started
// in dir, or "". Symlinked paths to the same folder match (/tmp is
// /private/tmp on macOS).
func sessionIn(sessions []protocol.Session, dir string) string {
	resolved, err := filepath.EvalSymlinks(dir)
	for _, s := range sessions {
		if filepath.Clean(s.Dir) == filepath.Clean(dir) {
			return s.Name
		}
		if other, otherErr := filepath.EvalSymlinks(s.Dir); err == nil && otherErr == nil && other == resolved {
			return s.Name
		}
	}
	return ""
}

func createDirectorySession(socket, cwd string, sessions []protocol.Session) (string, error) {
	used := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		used[s.Name] = true
	}
	base := directorySessionBase(cwd)
	for number := 1; ; number++ {
		name := directorySessionCandidate(base, number)
		if used[name] {
			continue
		}
		r, err := Request(socket, protocol.Request{Op: "create", Name: name, Dir: cwd})
		if err == nil {
			return name, nil
		}
		if r.Error != "session already exists" {
			return "", err
		}
		used[name] = true
	}
}

func directorySessionBase(cwd string) string {
	base := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r) {
			return r
		}
		return '-'
	}, filepath.Base(filepath.Clean(cwd)))
	base = strings.Trim(base, "-")
	if base == "" || base == "." || base == ".." {
		return "session"
	}
	return base
}

// freeDirectorySessionName is dir's folder name, numbered past names the
// sessions already use, as createDirectorySession would pick it.
func freeDirectorySessionName(dir string, sessions []protocol.Session) string {
	used := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		used[s.Name] = true
	}
	base := directorySessionBase(dir)
	for number := 1; ; number++ {
		if name := directorySessionCandidate(base, number); !used[name] {
			return name
		}
	}
}

func directorySessionCandidate(base string, number int) string {
	suffix := ""
	if number > 1 {
		suffix = fmt.Sprintf("-%d", number)
	}
	// Session names have an 80-byte wire limit. Keep UTF-8 intact and reserve
	// suffix space, including when the directory's name itself is very long.
	limit := 80 - len(suffix)
	for len(base) > limit {
		_, size := utf8.DecodeLastRuneInString(base)
		base = base[:len(base)-size]
	}
	return base + suffix
}
