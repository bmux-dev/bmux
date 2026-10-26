package client

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Git worktrees can live anywhere, so bmux finds a worktree's main repository
// from the worktree itself: its .git is a file naming a gitdir inside the main
// repository (<main>/.git/worktrees/<name>), and that gitdir's commondir file
// leads back to <main>/.git. Nothing is configured and git is never run.

// repoInfo describes the git repository holding a directory.
type repoInfo struct {
	Top      string // directory holding the .git entry
	Root     string // the main repository: Top itself unless Worktree
	Branch   string // checked-out branch, or a short commit when detached
	Worktree bool   // Top is a linked worktree of Root
}

// repoCacheTTL bounds how stale the sidebar and picker may show a branch.
// They resolve every session on every redraw.
const repoCacheTTL = 5 * time.Second

var repoCache = struct {
	sync.Mutex
	entries map[string]cachedRepo
}{entries: make(map[string]cachedRepo)}

type cachedRepo struct {
	info repoInfo
	at   time.Time
}

// resolveRepo is readRepo with a short-lived cache.
func resolveRepo(dir string) repoInfo {
	if !filepath.IsAbs(dir) {
		return repoInfo{}
	}
	repoCache.Lock()
	defer repoCache.Unlock()
	if c, ok := repoCache.entries[dir]; ok && time.Since(c.at) < repoCacheTTL {
		return c.info
	}
	if len(repoCache.entries) > 1024 {
		clear(repoCache.entries)
	}
	info := readRepo(dir)
	repoCache.entries[dir] = cachedRepo{info, time.Now()}
	return info
}

// readRepo finds the repository at or above dir. It returns the zero value
// outside any repository.
func readRepo(dir string) repoInfo {
	if !filepath.IsAbs(dir) {
		return repoInfo{}
	}
	for d := filepath.Clean(dir); ; {
		if fi, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return repoAt(d, fi)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return repoInfo{}
		}
		d = parent
	}
}

func repoAt(top string, gitEntry os.FileInfo) repoInfo {
	gitPath := filepath.Join(top, ".git")
	info := repoInfo{Top: realPath(top), Root: realPath(top)}
	if gitEntry.IsDir() {
		info.Branch = headBranch(gitPath)
		return info
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return repoInfo{}
	}
	line, _, _ := strings.Cut(string(data), "\n")
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return repoInfo{}
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(top, gitdir) // git may write it relative to the worktree
	}
	info.Branch = headBranch(gitdir)
	common, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return info // a submodule or a repository with a separate gitdir: its own project
	}
	commonDir := strings.TrimSpace(string(common))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitdir, commonDir)
	}
	root := filepath.Clean(commonDir)
	if filepath.Base(root) == ".git" {
		root = filepath.Dir(root)
	}
	info.Root, info.Worktree = realPath(root), true
	return info
}

// headBranch reads gitdir/HEAD: the branch name, or the first seven digits of
// a detached commit.
func headBranch(gitdir string) string {
	data, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	return head[:min(7, len(head))]
}

// worktreeDirs lists the linked worktrees of the repository at root that
// still exist, in the order of their registration names.
func worktreeDirs(root string) []string {
	registry := filepath.Join(root, ".git", "worktrees")
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		registry = filepath.Join(root, "worktrees") // a bare repository
	}
	entries, err := os.ReadDir(registry)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(registry, e.Name(), "gitdir"))
		if err != nil {
			continue
		}
		dir := filepath.Dir(strings.TrimSpace(string(data))) // the file names <worktree>/.git
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// realPath resolves symlinks so one folder reached two ways compares equal
// (/tmp is /private/tmp on macOS).
func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// projectName is how a repository's group is titled: its folder name, or for
// a bare repository the folder holding it.
func projectName(root string) string {
	name := filepath.Base(root)
	if name == ".bare" || name == ".git" {
		name = filepath.Base(filepath.Dir(root))
	}
	if trimmed := strings.TrimSuffix(name, ".git"); trimmed != "" {
		return trimmed
	}
	return name
}

// worktreeSessionName names a session started in a linked worktree. The
// folder's own name is kept when it tells worktrees apart. Workflows that put
// every checkout in <ticket>/repo make it ambiguous, so it becomes
// <project>-<parent folder>. It returns "" outside worktrees and when the
// folder name is already distinctive.
func worktreeSessionName(dir string) string {
	info := resolveRepo(dir)
	if !info.Worktree || info.Top != realPath(dir) {
		return ""
	}
	base := filepath.Base(info.Top)
	siblings := append([]string{info.Root}, worktreeDirs(info.Root)...)
	for _, other := range siblings {
		if realPath(other) != info.Top && filepath.Base(other) == base {
			return projectName(info.Root) + "-" + filepath.Base(filepath.Dir(info.Top))
		}
	}
	return ""
}
