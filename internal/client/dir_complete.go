package client

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// completeDir completes the last path segment of input against the
// directories that exist, like a shell: one match completes to it plus a
// slash, several complete to their longest common prefix. candidates lists
// the matches (each with a trailing slash) whenever there is more than one.
// The text keeps the form it was typed in, so ~/co gives ~/code/, and
// relative input is read relative to cwd.
func completeDir(input, cwd string) (completed string, candidates []string) {
	if input == "~" {
		return "~/", nil
	}
	cut := strings.LastIndexByte(input, '/') + 1
	parent, prefix := input[:cut], input[cut:]
	entries, err := os.ReadDir(startDirectory(parent, cwd))
	if err != nil {
		return input, nil
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(startDirectory(parent, cwd), name))
			isDir = err == nil && info.IsDir()
		}
		if isDir {
			names = append(names, name)
		}
	}
	switch len(names) {
	case 0:
		return input, nil
	case 1:
		return parent + names[0] + "/", nil
	}
	sort.Strings(names)
	common := names[0]
	for _, name := range names[1:] {
		n := 0
		for n < len(common) && n < len(name) && common[n] == name[n] {
			n++
		}
		common = common[:n]
	}
	for _, name := range names {
		candidates = append(candidates, name+"/")
	}
	return parent + common, candidates
}

// completeProjectDir is Tab in the Create form's directory field: the first
// Tab extends the text as far as the matches agree, and a Tab that can't
// extend it lists the candidates.
func (u *ui) completeProjectDir() {
	completed, candidates := completeDir(u.projects.dir, u.formBase())
	u.projects.formErr = ""
	if completed != u.projects.dir {
		u.projects.dir, u.projects.candidates = completed, nil
		return
	}
	u.projects.candidates = candidates
}

// wrapNames lays names out left to right in lines of at most width cells.
func wrapNames(names []string, width int) []string {
	var lines []string
	line := ""
	for _, name := range names {
		switch {
		case line == "":
			line = name
		case displayWidth(line)+2+displayWidth(name) <= width:
			line += "  " + name
		default:
			lines = append(lines, line)
			line = name
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
