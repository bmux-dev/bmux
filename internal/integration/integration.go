// Package integration installs, updates and removes the hooks, plugins and
// extensions that let coding agents report their own state (working,
// blocked, idle) and exact session to bmux through `bmux agent`. They change
// the agent's own config files, so nothing is installed unless asked for.
// Every file bmux writes carries a "bmux-integration: NAME vN" header, which
// is how updates are detected; other settings in edited files are preserved.
package integration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

// Status is how far an agent's integration is set up.
type Status int

const (
	// NotFound: the agent's command isn't on PATH and nothing is installed.
	NotFound Status = iota
	// Available: the agent is on PATH, without the integration.
	Available
	// Outdated: an older version is installed.
	Outdated
	// Installed: the current version is installed.
	Installed
)

func (s Status) String() string {
	return [...]string{"not found", "available", "update available", "installed"}[s]
}

// Env is where integrations look: the home directory, environment variables
// that move agent config directories, and PATH lookup.
type Env struct {
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

// System is the current user's environment.
func System() (Env, error) {
	home, err := os.UserHomeDir()
	return Env{Home: home, Getenv: os.Getenv, LookPath: exec.LookPath}, err
}

var agents = []Agent{
	claude.agent("Claude Code", "claude", "CLAUDE_CONFIG_DIR", ".claude", ""),
	codex.agent("Codex", "codex", "CODEX_HOME", ".codex", "Codex runs new hooks only after you trust them: open /hooks in Codex once."),
	omp.agent("omp", "omp", ompDir, "Restart omp to load it."),
	opencode.agent("opencode", "opencode", opencodeDir, "Restart opencode to load it."),
	pi.agent("pi", "pi", piDir, "Restart pi to load it."),
}

// Agent is one supported agent's integration.
type Agent struct {
	Name    string // used on the command line: bmux integration install NAME
	Label   string // shown to people
	Command string // the agent's program
	Version int
	Notes   string // what to do after installing, if anything

	configDir func(Env) string
	check     func(Env) error // refuses an install that would misfire; may be nil
	install   func(dir string) error
	uninstall func(dir string) error
	// installed returns the installed version, or 0.
	installed func(dir string) (int, error)
}

// All lists the supported agents in the order they're shown.
func All() []Agent { return agents }

// Find returns the agent with this command-line name.
func Find(name string) (Agent, bool) {
	for _, a := range agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// ConfigDir is the directory whose files the integration changes.
func (a Agent) ConfigDir(e Env) string { return a.configDir(e) }

// Status reports whether the integration is installed and current. A file
// that can't be read counts as not installed, with its error.
func (a Agent) Status(e Env) (Status, error) {
	version, err := a.installed(a.configDir(e))
	switch {
	case version == a.Version:
		return Installed, err
	case version > 0:
		return Outdated, err
	}
	if _, lookErr := e.LookPath(a.Command); lookErr == nil {
		return Available, err
	}
	return NotFound, err
}

// Install writes the current version, replacing an older one.
func (a Agent) Install(e Env) error {
	if a.check != nil {
		if err := a.check(e); err != nil {
			return err
		}
	}
	return a.install(a.configDir(e))
}

// Uninstall removes only what bmux installed.
func (a Agent) Uninstall(e Env) error { return a.uninstall(a.configDir(e)) }

var header = regexp.MustCompile(`bmux-integration: ([a-z]+) v([0-9]+)`)

// fileVersion reads a managed file's version: 0 when it's missing or isn't
// this agent's bmux file.
func fileVersion(path, name string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	m := header.FindSubmatch(data)
	if m == nil || string(m[1]) != name {
		return 0, nil
	}
	version, _ := strconv.Atoi(string(m[2]))
	return version, nil
}

// headerLines marks a managed file; prefix is the language's line comment.
func headerLines(prefix, name string, version int) string {
	return fmt.Sprintf("%[1]s bmux-integration: %[2]s v%[3]d\n"+
		"%[1]s Managed by bmux: `bmux integration install %[2]s` overwrites this file and\n"+
		"%[1]s `bmux integration uninstall %[2]s` removes it. Add your own hooks elsewhere.\n", prefix, name, version)
}

// writeFile replaces path atomically, keeping an existing file's mode, and
// creates its directory if needed.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".bmux-*")
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
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// removeFile deletes a managed file, if it is one of ours.
func removeFile(path, name string) error {
	if version, err := fileVersion(path, name); err != nil || version == 0 {
		return err
	}
	return os.Remove(path)
}

// dirFromEnv is the directory in variable, else under the home directory.
func dirFromEnv(variable string, parts ...string) func(Env) string {
	return func(e Env) string {
		if dir := e.Getenv(variable); dir != "" {
			return dir
		}
		return filepath.Join(append([]string{e.Home}, parts...)...)
	}
}
