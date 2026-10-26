package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// removedCommands were dropped on purpose, with the reason; any other
// command a release's bmux --help listed must still exist with the flags it
// listed. See testdata/releases and the compatibility page.
var removedCommands = map[string]string{}

var (
	helpLine  = regexp.MustCompile(`^  bmux ([a-z][a-z-]*)(.*)$`)
	usageFlag = regexp.MustCompile(`(?:^|[\s\[|(])(--?[a-z][a-z-]*)`)
)

// releaseCommands reads the commands, with their flags, a release's
// bmux --help listed.
func releaseCommands(t *testing.T, path string) map[string][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	commands := make(map[string][]string)
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		m := helpLine.FindStringSubmatch(lines.Text())
		if m == nil {
			continue
		}
		// The usage ends where the summary's column starts, two spaces on.
		usage, _, _ := strings.Cut(strings.TrimSpace(m[2]), "  ")
		commands[m[1]] = nil
		for _, f := range usageFlag.FindAllStringSubmatch(" "+usage, -1) {
			commands[m[1]] = append(commands[m[1]], f[1])
		}
	}
	return commands
}

func TestEveryReleasedCommandAndFlagStillExists(t *testing.T) {
	helps, err := filepath.Glob("../../testdata/releases/*/help.txt")
	if err != nil || len(helps) == 0 {
		t.Fatalf("no release fixtures: %v", err)
	}
	for _, help := range helps {
		release := filepath.Base(filepath.Dir(help))
		for name, flags := range releaseCommands(t, help) {
			if _, removed := removedCommands[name]; removed {
				continue
			}
			c, ok := findCommand(name)
			if !ok {
				t.Errorf("%s's bmux %s is gone; restore it or list it in removedCommands with the reason", release, name)
				continue
			}
			for _, f := range flags {
				if !regexp.MustCompile(`(^|[\s\[|(])` + regexp.QuoteMeta(f) + `($|[\s\]|)=])`).MatchString(c.usage) {
					t.Errorf("%s's bmux %s %s is gone from its usage %q", release, name, f, c.usage)
				}
			}
		}
	}
}
