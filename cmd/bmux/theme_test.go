package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josephferrero/bmux/internal/config"
)

func runThemeIn(t *testing.T, content string, env map[string]string, args ...string) (file, stdout, stderr string, err error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("BMUX_CONFIG", path)
	var out, errOut bytes.Buffer
	// No server listens on this socket, as when a hook runs outside bmux.
	err = themeCommand(filepath.Join(t.TempDir(), "absent.sock"), args, &out, &errOut, func(k string) string { return env[k] })
	data, _ := os.ReadFile(path)
	return string(data), out.String(), errOut.String(), err
}

func TestThemeSavesTheResolvedNameAndKeepsTheFile(t *testing.T) {
	file, out, _, err := runThemeIn(t, "# mine\ntheme = \"dark\"  # keep\n\n[sidebar]\nwidth = 30\n", nil, "Catppuccin")
	if err != nil || file != "# mine\ntheme = \"catppuccin-mocha\" # keep\n\n[sidebar]\nwidth = 30\n" {
		t.Fatalf("file %q, err %v", file, err)
	}
	if !strings.Contains(out, "catppuccin-mocha") || !strings.Contains(out, "no server is running") {
		t.Fatalf("output %q", out)
	}
	if file, _, _, err = runThemeIn(t, "", nil, "gruvbox-light"); err != nil || file != "theme = \"gruvbox-light\"\n" {
		t.Fatalf("new file %q, err %v", file, err)
	}
}

func TestThemeRejectsUnknownNamesWithoutTouchingTheFile(t *testing.T) {
	const original = "theme = \"nord\"\n"
	for _, args := range [][]string{{"neon"}, {"--bogus"}, {"nord", "dracula"}} {
		file, _, _, err := runThemeIn(t, original, nil, args...)
		if err == nil || file != original {
			t.Errorf("bmux theme %v: err %v, file %q", args, err, file)
		}
	}
}

func TestThemeReportsAndListsWithoutWriting(t *testing.T) {
	file, out, _, err := runThemeIn(t, "theme = \"dracula\"\n", nil)
	if err != nil || out != "dracula\n" || file != "theme = \"dracula\"\n" {
		t.Fatalf("current theme: %q %q %v", out, file, err)
	}
	if _, out, _, err = runThemeIn(t, "", nil, "--list"); err != nil || strings.Fields(out)[0] != config.ThemeNames[0] || len(strings.Fields(out)) != len(config.ThemeNames) {
		t.Fatalf("list: %q %v", out, err)
	}
}

func TestThemeWarnsWhenTheEnvironmentOverridesTheFile(t *testing.T) {
	_, _, errOut, err := runThemeIn(t, "", map[string]string{"BMUX_THEME": "light"}, "nord")
	if err != nil || !strings.Contains(errOut, "BMUX_THEME=light") {
		t.Fatalf("stderr %q, err %v", errOut, err)
	}
}
