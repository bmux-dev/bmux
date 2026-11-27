package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkOnboardedKeepsTheRestOfTheFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "bmux", "config.toml")
	if err := MarkOnboarded(missing); err != nil {
		t.Fatal(err)
	}
	if cfg, err := Load(missing, noEnv); err != nil || cfg.Onboarding {
		t.Fatalf("new file: onboarding=%v %v", cfg.Onboarding, err)
	}

	user := "# my settings\ntheme = \"light\"\n\n[sidebar]\nwidth = 30\nonboarding = true\n"
	path := write(t, user)
	if err := MarkOnboarded(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if rest, ok := strings.CutPrefix(string(data), onboardedLine); !ok || rest != user {
		t.Fatalf("file not kept as written:\n%s", data)
	}

	// A top-level onboarding line is replaced, not duplicated.
	path = write(t, "prefix = \"C-b\"\nonboarding = true # show it\n[ui]\naccent = \"green\"\n")
	if err := MarkOnboarded(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "prefix = \"C-b\"\n"+onboardedLine+"[ui]\naccent = \"green\"\n" {
		t.Fatalf("existing line not replaced:\n%s", data)
	}
	cfg, err := Load(path, noEnv)
	if err != nil || cfg.Onboarding || cfg.Prefix != "C-b" || cfg.UI.Accent != "green" {
		t.Fatalf("after marking: %+v %v", cfg, err)
	}

	// A file that doesn't parse is left alone.
	broken := "theme = \n"
	path = write(t, broken)
	if err := MarkOnboarded(path); err == nil {
		t.Fatal("rewrote a file that doesn't parse")
	}
	if data, _ := os.ReadFile(path); string(data) != broken {
		t.Fatalf("broken file changed: %q", data)
	}
}

func TestMarkOnboardedPreservesSymlink(t *testing.T) {
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	configDir := filepath.Join(root, "xdg", "bmux")
	if err := os.MkdirAll(dotfilesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(dotfilesDir, "config.toml")
	if err := os.WriteFile(targetPath, []byte("theme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(configDir, "config.toml")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Fatal(err)
	}

	if err := MarkOnboarded(linkPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to remain a symlink", linkPath)
	}
	dest, err := os.Readlink(linkPath)
	if err != nil || dest != targetPath {
		t.Fatalf("symlink target = %q (%v), want %q", dest, err, targetPath)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "onboarding = false") || !strings.Contains(string(data), "theme = \"dark\"") {
		t.Fatalf("target file missing expected content:\n%s", data)
	}
}
