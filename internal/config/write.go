package config

import (
	"os"
	"path/filepath"
	"strings"
)

// writeAtomic replaces the file at path with data, keeping its permissions,
// so a reader never sees half a config file.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.toml.bmux-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// setLine sets key = literal in the given table of TOML text ("" is the top
// level) and returns the new text. It edits in place so comments and layout
// survive, in this order of preference:
//
//  1. an active "key = value" line, which keeps its trailing comment;
//  2. a commented "# key = value" line, as in bmux default-config, which is
//     uncommented;
//  3. a new line at the top of the table (or the end of the top level);
//  4. a new table at the end of the file.
//
// A value that spans lines, such as an array, is replaced whole. The caller
// checks that the result parses and holds the value.
func setLine(text, table, key, literal string) string {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		lines[n-1] += "\n"
	}
	start, end, found := 0, len(lines), table == ""
	for i, line := range lines {
		name, ok := tableHeader(line)
		if !ok {
			continue
		}
		if found {
			end = i
			break
		}
		if name == table {
			start, found = i+1, true
		}
	}
	if !found {
		out := strings.Join(lines, "")
		if out != "" {
			out += "\n"
		}
		return out + "[" + table + "]\n" + key + " = " + literal + "\n"
	}
	commented := -1
	for i := start; i < end; i++ {
		name, rest, active := splitAssignment(lines[i])
		if name != key {
			continue
		}
		if active {
			return replaceLines(lines, i, valueEnd(lines, i, end), key, literal, rest, 0)
		}
		if commented < 0 {
			commented = i
		}
	}
	if commented >= 0 {
		_, rest, _ := splitAssignment(lines[commented])
		return replaceLines(lines, commented, commented+1, key, literal, rest, 2)
	}
	at := start
	if table == "" {
		// Keep the blank line that separates the top level from the first table.
		for at = end; at > start && strings.TrimSpace(lines[at-1]) == ""; at-- {
		}
	}
	inserted := append([]string{}, lines[:at]...)
	inserted = append(inserted, key+" = "+literal+"\n")
	return strings.Join(append(inserted, lines[at:]...), "")
}

// tableHeader returns the name in a "[name]" line. Arrays of tables
// ("[[name]]") report a name no real table has.
func tableHeader(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	closing := strings.Index(trimmed, "]")
	if closing < 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[1:closing]), true
}

// splitAssignment reads "key = value" and "# key = value" lines. rest is
// everything after the "=".
func splitAssignment(line string) (key, rest string, active bool) {
	trimmed := strings.TrimSpace(line)
	active = !strings.HasPrefix(trimmed, "#")
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	key, rest, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), rest, active
}

// valueEnd is the line after the one where the value starting on line i
// ends: i+1 unless it's an array that continues.
func valueEnd(lines []string, i, limit int) int {
	_, rest, _ := splitAssignment(lines[i])
	if value, _ := splitComment(rest); !strings.HasPrefix(strings.TrimSpace(value), "[") || strings.Contains(value, "]") {
		return i + 1
	}
	for j := i + 1; j < limit; j++ {
		if value, _ := splitComment(lines[j]); strings.Contains(value, "]") {
			return j + 1
		}
	}
	return limit
}

// splitComment separates a value from its trailing "# comment", ignoring
// a # inside a quoted string.
func splitComment(s string) (value, comment string) {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch {
		case quoted && s[i] == '\\':
			i++
		case s[i] == '"':
			quoted = !quoted
		case !quoted && s[i] == '#':
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// replaceLines swaps lines[from:to] for "key = literal", with the old
// line's trailing comment kept at its column. shift is how many columns the
// old line had before its key that the new one doesn't (a "# " prefix).
func replaceLines(lines []string, from, to int, key, literal, rest string, shift int) string {
	line := key + " = " + literal
	if _, comment := splitComment(rest); comment != "" && to == from+1 {
		old := strings.TrimRight(lines[from], "\r\n")
		column := strings.LastIndex(old, comment) - shift
		line += strings.Repeat(" ", max(1, column-len(line))) + strings.TrimRight(comment, "\r\n")
	}
	out := append([]string{}, lines[:from]...)
	out = append(out, line+"\n")
	return strings.Join(append(out, lines[to:]...), "")
}
