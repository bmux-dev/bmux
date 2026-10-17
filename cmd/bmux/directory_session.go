package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/protocol"
)

// Bare bmux always creates fresh work, never reattaches to a similarly named
// session. The server's create operation arbitrates concurrent name collisions.
func createDirectorySession(socket, cwd string) (string, error) {
	r, err := client.Request(socket, protocol.Request{Op: "list"})
	if err != nil {
		return "", err
	}
	used := make(map[string]bool, len(r.Sessions))
	for _, s := range r.Sessions {
		used[s.Name] = true
	}
	base := directorySessionBase(cwd)
	for number := 1; ; number++ {
		name := directorySessionCandidate(base, number)
		if used[name] {
			continue
		}
		r, err := client.Request(socket, protocol.Request{Op: "create", Name: name, Dir: cwd})
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
